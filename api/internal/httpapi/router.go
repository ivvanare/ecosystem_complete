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
				adminHandler := &adminHandlers{reader: opts.Admin}
				mux.HandleFunc("GET /v1/admin/users", admin(adminHandler.users))
				mux.HandleFunc("PATCH /v1/admin/users/{userId}/roles", admin(adminHandler.setRoles))
				mux.HandleFunc("GET /v1/admin/audit-events", admin(adminHandler.auditEvents))
			}
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
