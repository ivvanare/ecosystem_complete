// Package httpapi wires the HTTP surface of the service.
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// Probe reports the reachability of one external dependency.
type Probe interface {
	Name() string
	Required() bool
	Check(ctx context.Context) error
}

// Options configures the router.
type Options struct {
	Probes             []Probe
	ProbeTimeout       time.Duration
	Logger             *slog.Logger
	CORSAllowedOrigins []string
	Signup             SignupRequester
	SignupVerifier     SignupVerifier
	Login              LoginAuthenticator
	Rotator            SessionRotator
	CookieSecure       bool
	TokenParser        TokenParser
	Authorizer         Authorizer
	Identity           IdentityReader
	Admin              AdminReader
	Invitations        InvitationSender
	Password           PasswordService
	EmailChange        EmailChangeService
	Social             SocialAuthenticator
	Discovery          PlaceDiscovery
	Follows            SocialGraph
	Checkins           Checkins
	PlacesAdmin        PlaceBackoffice
	Plans              PlanCollaboration
	Reviews            ReviewService
}

const defaultProbeTimeout = 2 * time.Second

// New builds the HTTP handler for the service.
func New(opts Options) http.Handler {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	timeout := opts.ProbeTimeout
	if timeout <= 0 {
		timeout = defaultProbeTimeout
	}

	h := &health{probes: opts.Probes, timeout: timeout, logger: logger}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", h.live)
	mux.HandleFunc("GET /health/ready", h.ready)

	auth := &authHandlers{
		signup:   opts.Signup,
		verifier: opts.SignupVerifier,
		login:    opts.Login,
		rotator:  opts.Rotator,
		cookies:  cookieSettings{allowedOrigins: opts.CORSAllowedOrigins, secure: opts.CookieSecure},
		logger:   logger,
	}
	if opts.Signup != nil {
		mux.HandleFunc("POST /v1/auth/signup", auth.startSignup)
	}
	// Independent of Signup: with the broker down a new registration cannot start, but a
	// user who already holds a code must still be able to finish.
	if opts.SignupVerifier != nil {
		mux.HandleFunc("POST /v1/auth/signup/verify", auth.verifySignup)
	}
	if opts.Login != nil {
		mux.HandleFunc("POST /v1/auth/login", auth.loginUser)
	}
	if opts.Rotator != nil {
		mux.HandleFunc("POST /v1/auth/refresh", auth.refreshSession)
		mux.HandleFunc("POST /v1/auth/logout", auth.logoutSession)
	}

	if opts.Password != nil {
		passwordHandler := &passwordHandlers{service: opts.Password}
		mux.HandleFunc("POST /v1/auth/password/forgot", passwordHandler.forgotPassword)
		mux.HandleFunc("POST /v1/auth/password/reset", passwordHandler.resetPassword)
		if opts.TokenParser != nil {
			mux.HandleFunc("POST /v1/auth/password/change",
				RequireAuth(opts.TokenParser, passwordHandler.changePassword))
		}
	}

	if opts.Social != nil {
		socialHandler := &socialHandlers{
			service: opts.Social,
			cookies: cookieSettings{allowedOrigins: opts.CORSAllowedOrigins, secure: opts.CookieSecure},
			logger:  logger,
		}
		mux.HandleFunc("POST /v1/auth/social", socialHandler.login)
	}

	if opts.EmailChange != nil && opts.TokenParser != nil {
		emailHandler := &emailChangeHandlers{service: opts.EmailChange}
		mux.HandleFunc("POST /v1/auth/email/change",
			RequireAuth(opts.TokenParser, emailHandler.request))
		mux.HandleFunc("POST /v1/auth/email/change/verify",
			RequireAuth(opts.TokenParser, emailHandler.verify))
	}

	if opts.Identity != nil && opts.TokenParser != nil {
		identityHandler := &identityHandlers{reader: opts.Identity}

		mux.HandleFunc("GET /v1/me",
			RequireAuth(opts.TokenParser, identityHandler.me))

		if opts.Authorizer != nil {
			admin := func(next http.HandlerFunc) http.HandlerFunc {
				return RequireAuth(opts.TokenParser, RequirePermission(opts.Authorizer, AdminPermission, logger, next))
			}
			mux.HandleFunc("GET /v1/admin/roles", admin(identityHandler.roles))
			mux.HandleFunc("GET /v1/admin/permissions", admin(identityHandler.permissions))

			if opts.Admin != nil {
				adminHandler := &adminHandlers{
					reader:      opts.Admin,
					password:    opts.Password,
					invitations: opts.Invitations,
				}
				mux.HandleFunc("GET /v1/admin/users", admin(adminHandler.users))
				mux.HandleFunc("PATCH /v1/admin/users/{userId}", admin(adminHandler.updateUser))
				// Both routes need the broker to deliver a message, so they are absent
				// rather than accepted-and-dropped when it is not configured.
				if opts.Invitations != nil {
					mux.HandleFunc("POST /v1/admin/users/invitations", admin(adminHandler.inviteUser))
				}
				if opts.Password != nil {
					mux.HandleFunc("POST /v1/admin/users/{userId}/password-reset", admin(adminHandler.resetUserPassword))
				}
				mux.HandleFunc("PATCH /v1/admin/users/{userId}/roles", admin(adminHandler.setRoles))
				mux.HandleFunc("GET /v1/admin/audit-events", admin(adminHandler.auditEvents))
			}
		}
	}

	// Discovery is public and read-only (S14): no auth middleware, and absent rather
	// than accepted-and-failed when the read path is not wired.
	if opts.Discovery != nil {
		placesHandler := &placesHandlers{discovery: opts.Discovery, logger: logger}
		mux.HandleFunc("GET /v1/places", placesHandler.places)
	}

	// Plans (P3a): the mobile consumer's collaborative planning (D21). Authorization is
	// per-plan role, so these routes take auth but no capability permission.
	if opts.Plans != nil {
		plansHandler := &planHandlers{service: opts.Plans, logger: logger}
		mux.HandleFunc("POST /v1/plans", RequireAuth(opts.TokenParser, plansHandler.create))
		mux.HandleFunc("GET /v1/plans", RequireAuth(opts.TokenParser, plansHandler.listed))
		mux.HandleFunc("GET /v1/plans/{coRoute}", RequireAuth(opts.TokenParser, plansHandler.read))
		mux.HandleFunc("PATCH /v1/plans/{coRoute}", RequireAuth(opts.TokenParser, plansHandler.update))
		mux.HandleFunc("POST /v1/plans/{coRoute}/members", RequireAuth(opts.TokenParser, plansHandler.join))
		mux.HandleFunc("DELETE /v1/plans/{coRoute}/members/{coUser}", RequireAuth(opts.TokenParser, plansHandler.leave))
		mux.HandleFunc("GET /v1/plans/{coRoute}/places", RequireAuth(opts.TokenParser, plansHandler.places))
		mux.HandleFunc("POST /v1/plans/{coRoute}/places", RequireAuth(opts.TokenParser, plansHandler.addPlace))
		mux.HandleFunc("DELETE /v1/plans/{coRoute}/places/{coPlace}", RequireAuth(opts.TokenParser, plansHandler.removePlace))
		mux.HandleFunc("POST /v1/plans/{coRoute}/places/{coPlace}/votes", RequireAuth(opts.TokenParser, plansHandler.vote))
	}

	// The category catalogue is public taxonomy: the panel renders a select from it and the
	// mobile client renders the same list as discovery filters.
	if opts.PlacesAdmin != nil {
		categoriesHandler := &placesAdminHandlers{service: opts.PlacesAdmin, logger: logger}
		mux.HandleFunc("GET /v1/place-categories", categoriesHandler.categories)
	}

	// Places backoffice (place-ownership): the catalogue is curated, so writing it is
	// gated by admin.access for creation and by the ownership viewer for the rest.
	if opts.PlacesAdmin != nil {
		backoffice := &placesAdminHandlers{
			service: opts.PlacesAdmin, parser: opts.TokenParser,
			authorizer: opts.Authorizer, logger: logger,
		}
		mux.HandleFunc("GET /v1/admin/places",
			RequireAuth(opts.TokenParser,
				RequirePermission(opts.Authorizer, PlaceUpdatePermission, logger, backoffice.listed)))
		mux.HandleFunc("GET /v1/admin/places/{coPlace}",
			RequireAuth(opts.TokenParser,
				RequirePermission(opts.Authorizer, PlaceUpdatePermission, logger, backoffice.read)))
		// Creation is the operator capability, not admin.access: the seeded EDITOR role
		// holds admin.access (it means "may enter the backoffice"), and creating a curated
		// catalogue entry is not something every editor may do.
		mux.HandleFunc("POST /v1/admin/places",
			RequireAuth(opts.TokenParser,
				RequirePermission(opts.Authorizer, PlaceManageAllPermission, logger, backoffice.create)))
		mux.HandleFunc("PATCH /v1/admin/places/{coPlace}",
			RequireAuth(opts.TokenParser,
				RequirePermission(opts.Authorizer, PlaceUpdatePermission, logger, backoffice.update)))
		mux.HandleFunc("DELETE /v1/admin/places/{coPlace}",
			RequireAuth(opts.TokenParser,
				RequirePermission(opts.Authorizer, PlaceDeletePermission, logger, backoffice.retire)))
	}

	// Social graph: the consumer is the mobile app (D21), so these are authenticated
	// routes with no admin analogue. Absent when unwired, like the broker-gated routes.
	if opts.Follows != nil {
		graph := &graphHandlers{follows: opts.Follows, checkins: opts.Checkins, parser: opts.TokenParser, logger: logger}
		mux.HandleFunc("POST /v1/follows", graph.createFollow)
		mux.HandleFunc("DELETE /v1/follows/{coUser}", graph.deleteFollow)
		mux.HandleFunc("GET /v1/follows", graph.listFollows)
		if opts.Checkins != nil {
			mux.HandleFunc("POST /v1/places/{coPlace}/checkins", graph.createCheckin)
		}
	}

	// Reviews are the first domain write path (p1-discovery.md §6). The list is
	// public; mutations require the bearer, and moderation reuses the seeded
	// review.moderate capability (S12). Like the broker-gated routes, the routes are
	// absent rather than accepted-and-failed when the service is not wired (T10).
	if opts.Reviews != nil && opts.TokenParser != nil {
		reviewsHandler := &reviewsHandlers{
			service:    opts.Reviews,
			parser:     opts.TokenParser,
			authorizer: opts.Authorizer,
			logger:     logger,
		}
		mux.HandleFunc("GET /v1/places/{coPlace}/reviews", reviewsHandler.list)
		mux.HandleFunc("POST /v1/places/{coPlace}/reviews",
			RequireAuth(opts.TokenParser, reviewsHandler.create))
		mux.HandleFunc("PATCH /v1/places/{coPlace}/reviews/{coReview}",
			RequireAuth(opts.TokenParser, reviewsHandler.update))
		mux.HandleFunc("DELETE /v1/places/{coPlace}/reviews/{coReview}",
			RequireAuth(opts.TokenParser, reviewsHandler.delete))
		if opts.Authorizer != nil {
			mux.HandleFunc("PATCH /v1/admin/reviews/{coReview}",
				RequireAuth(opts.TokenParser,
					RequirePermission(opts.Authorizer, ReviewModeratePermission, logger, reviewsHandler.moderate)))
			// The listing exists so a moderator can discover a coReview at all: the
			// moderation route above is useless without it.
			mux.HandleFunc("GET /v1/admin/reviews",
				RequireAuth(opts.TokenParser,
					RequirePermission(opts.Authorizer, ReviewModeratePermission, logger, reviewsHandler.adminList)))
		}
	}

	mux.HandleFunc("/", notFound)

	return CORS(opts.CORSAllowedOrigins)(mux)
}

func notFound(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotFound, errorEnvelope{
		Error: errorBody{Code: "not_found", Message: "Not found."},
	})
}

// errorBody is the only error shape the API exposes. api/openapi.yaml defines it and
// every endpoint must reuse it: clients parse `code`, never `message`.
type errorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
