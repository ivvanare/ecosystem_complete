package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The panel is a SPA on another origin, so every call is cross-origin and the
// Authorization header forces a preflight. These tests pin the three things that break
// silently in a browser: a wildcard origin with credentials, a preflight that requires
// auth, and a missing Vary: Origin.
func newCORSHandler(origins ...string) http.Handler {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return CORS(origins)(next)
}

func TestPreflightFromAllowedOriginIsAnsweredWithoutAuth(t *testing.T) {
	handler := newCORSHandler("https://admin.melia.test")

	req := httptest.NewRequest(http.MethodOptions, "/v1/auth/login", nil)
	req.Header.Set("Origin", "https://admin.melia.test")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "authorization,content-type")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://admin.melia.test" {
		t.Errorf("Allow-Origin = %q, want the exact origin", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Allow-Credentials = %q, want true (the refresh cookie needs it)", got)
	}
	for _, header := range []string{"Authorization", "Content-Type", "X-CSRF-Token"} {
		if !containsHeader(rec.Header().Get("Access-Control-Allow-Headers"), header) {
			t.Errorf("Allow-Headers %q does not list %s", rec.Header().Get("Access-Control-Allow-Headers"), header)
		}
	}
	if rec.Body.Len() != 0 {
		t.Errorf("preflight wrote a body: %q", rec.Body.String())
	}
}

func TestPreflightFromUnknownOriginGetsNoCORSHeaders(t *testing.T) {
	handler := newCORSHandler("https://admin.melia.test")

	req := httptest.NewRequest(http.MethodOptions, "/v1/auth/login", nil)
	req.Header.Set("Origin", "https://evil.test")
	req.Header.Set("Access-Control-Request-Method", "POST")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want empty for a disallowed origin", got)
	}
}

func TestActualRequestEchoesAllowedOriginAndVaries(t *testing.T) {
	handler := newCORSHandler("https://admin.melia.test")

	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Origin", "https://admin.melia.test")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://admin.melia.test" {
		t.Errorf("Allow-Origin = %q", got)
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Errorf("Vary = %q, want Origin (a cache must not reuse one origin's response for another)", got)
	}
}

func TestNoConfiguredOriginsMeansNoCORS(t *testing.T) {
	handler := newCORSHandler()

	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Origin", "https://admin.melia.test")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want empty when nothing is configured", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("Allow-Credentials = %q, want empty when nothing is configured", got)
	}
}

func TestRequestsWithoutOriginPassThroughUntouched(t *testing.T) {
	handler := newCORSHandler("https://admin.melia.test")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/me", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want empty for a same-origin or non-browser caller", got)
	}
}

func containsHeader(list, want string) bool {
	for _, part := range strings.Split(list, ",") {
		if strings.TrimSpace(part) == want {
			return true
		}
	}
	return false
}
