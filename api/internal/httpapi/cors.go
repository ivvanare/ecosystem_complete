package httpapi

import (
	"net/http"
	"slices"
	"strings"
)

// CORS answers browser preflights and echoes the caller's origin when it is allowed.
//
// Two details are not optional here. Credentials are enabled because the panel's refresh
// token travels in an httpOnly cookie, and with credentials a wildcard origin is invalid —
// so the origin is always echoed exactly from the configured list. And a preflight must
// never require authentication: an OPTIONS that answered 401 would break the SPA before it
// could send a token.
func CORS(allowed []string) func(http.Handler) http.Handler {
	normalized := make([]string, 0, len(allowed))
	for _, origin := range allowed {
		if origin = strings.TrimSpace(origin); origin != "" {
			normalized = append(normalized, origin)
		}
	}

	return func(next http.Handler) http.Handler {
		if len(normalized) == 0 {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" || !slices.Contains(normalized, origin) {
				next.ServeHTTP(w, r)
				return
			}

			header := w.Header()
			header.Set("Access-Control-Allow-Origin", origin)
			header.Set("Access-Control-Allow-Credentials", "true")
			// A shared cache must not answer one origin with another origin's response.
			header.Add("Vary", "Origin")

			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				header.Set("Access-Control-Allow-Methods", corsAllowMethods)
				header.Set("Access-Control-Allow-Headers", corsAllowHeaders)
				header.Set("Access-Control-Max-Age", corsMaxAge)
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

const (
	corsAllowMethods = "GET, POST, PATCH, DELETE, OPTIONS"
	corsAllowHeaders = "Authorization, Content-Type, X-CSRF-Token"
	corsMaxAge       = "600"
)
