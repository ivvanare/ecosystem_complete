package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/ivvanare/ecosystem_complete/api/internal/session"
)

type ctxKey string

const userIDKey ctxKey = "user_id"

// TokenParser validates an access token.
type TokenParser interface {
	Parse(raw string) (session.Claims, error)
}

// Authorizer answers whether a user currently holds a permission.
type Authorizer interface {
	Can(ctx context.Context, userID, permission string) (bool, error)
}

// RequireAuth rejects a request without a valid bearer token and puts the subject in the
// request context.
func RequireAuth(parser TokenParser, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
			return
		}

		claims, err := parser.Parse(token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
			return
		}

		ctx := context.WithValue(r.Context(), userIDKey, claims.Subject)
		next(w, r.WithContext(ctx))
	}
}

// RequirePermission guards a handler with a live permission check.
//
// The check reads the database on every request instead of trusting a claim: disabling a
// role must take effect immediately, not when the access token happens to expire.
func RequirePermission(authorizer Authorizer, permission string, logger *slog.Logger, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := userIDFrom(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
			return
		}

		allowed, err := authorizer.Can(r.Context(), userID, permission)
		if err != nil {
			logger.Error("permission check failed", slog.String("error", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
		if !allowed {
			writeError(w, http.StatusForbidden, "permission_denied", "You do not have permission to perform this action.")
			return
		}

		next(w, r)
	}
}

func bearerToken(r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(header[len(prefix):])
	return token, token != ""
}

func userIDFrom(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(userIDKey).(string)
	return userID, ok && userID != ""
}
