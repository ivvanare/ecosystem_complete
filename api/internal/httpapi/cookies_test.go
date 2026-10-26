package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ivvanare/ecosystem_complete/api/internal/session"
)

const panelOrigin = "http://localhost:4200"

func browserHandler(t *testing.T, rotator SessionRotator, login LoginAuthenticator) http.Handler {
	t.Helper()
	return New(Options{
		Login:              login,
		Rotator:            rotator,
		CORSAllowedOrigins: []string{panelOrigin},
		CookieSecure:       false, // the test client speaks http
		Logger:             slog.New(slog.DiscardHandler),
	})
}

func cookieNamed(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func TestBrowserLoginGetsTheRefreshTokenAsACookieAndNotInTheBody(t *testing.T) {
	auth := &fakeLogin{tokens: session.Tokens{
		AccessToken: "access", ExpiresIn: 900, RefreshToken: "refresh-secreto", RefreshExpiresIn: 2592000,
	}}
	handler := browserHandler(t, &fakeRotator{}, auth)

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(`{"email":"ana@melia.test","password":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", panelOrigin)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	refresh := cookieNamed(rec, refreshCookieName)
	if refresh == nil {
		t.Fatal("no refresh cookie was set")
	}
	if !refresh.HttpOnly {
		t.Error("the refresh cookie is readable by JavaScript")
	}
	if refresh.Value != "refresh-secreto" {
		t.Errorf("cookie value = %q", refresh.Value)
	}
	if refresh.Path != sessionCookiePath {
		t.Errorf("path = %q, want %q", refresh.Path, sessionCookiePath)
	}

	csrf := cookieNamed(rec, csrfCookieName)
	if csrf == nil || csrf.Value == "" {
		t.Fatal("no csrf cookie was set")
	}
	if csrf.HttpOnly {
		t.Error("the csrf cookie must be readable: double submit needs it")
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if _, present := body["refresh_token"]; present {
		t.Error("the refresh token leaked into the body for a browser client")
	}
	if body["access_token"] != "access" {
		t.Errorf("access token missing from the body: %v", body)
	}
}

func TestMobileLoginKeepsTheTokenInTheBody(t *testing.T) {
	auth := &fakeLogin{tokens: session.Tokens{AccessToken: "a", RefreshToken: "r", ExpiresIn: 900}}
	handler := browserHandler(t, &fakeRotator{}, auth)

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(`{"email":"ana@melia.test","password":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if cookieNamed(rec, refreshCookieName) != nil {
		t.Error("a non-browser client received a cookie")
	}
	if !strings.Contains(rec.Body.String(), `"refresh_token":"r"`) {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestForeignOriginIsTreatedAsANonBrowser(t *testing.T) {
	auth := &fakeLogin{tokens: session.Tokens{AccessToken: "a", RefreshToken: "r", ExpiresIn: 900}}
	handler := browserHandler(t, &fakeRotator{}, auth)

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(`{"email":"ana@melia.test","password":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.test")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if cookieNamed(rec, refreshCookieName) != nil {
		t.Error("an untrusted origin received a session cookie")
	}
}

func TestRefreshFromCookieRequiresTheCSRFToken(t *testing.T) {
	rotator := &fakeRotator{tokens: session.Tokens{AccessToken: "a2", RefreshToken: "r2", ExpiresIn: 900}}

	cases := []struct {
		name   string
		header string
		status int
	}{
		{"matching token", "csrf-value", http.StatusOK},
		{"missing header", "", http.StatusForbidden},
		{"mismatched header", "otro", http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := browserHandler(t, rotator, &fakeLogin{})

			req := httptest.NewRequest(http.MethodPost, "/v1/auth/refresh", strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(&http.Cookie{Name: refreshCookieName, Value: "r1"})
			req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-value"})
			if tc.header != "" {
				req.Header.Set(csrfHeaderName, tc.header)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.status, rec.Body.String())
			}
			if tc.status == http.StatusForbidden && !strings.Contains(rec.Body.String(), "csrf_invalid") {
				t.Errorf("body = %s", rec.Body.String())
			}
		})
	}
}

func TestRefreshFromTheBodyNeedsNoCSRF(t *testing.T) {
	rotator := &fakeRotator{tokens: session.Tokens{AccessToken: "a2", RefreshToken: "r2", ExpiresIn: 900}}
	handler := browserHandler(t, rotator, &fakeLogin{})

	rec := postJSONTo(t, handler, "/v1/auth/refresh", `{"refresh_token":"r1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if rotator.presented != "r1" {
		t.Errorf("presented = %q", rotator.presented)
	}
}

func TestLogoutFromCookieClearsTheSession(t *testing.T) {
	rotator := &fakeRotator{}
	handler := browserHandler(t, rotator, &fakeLogin{})

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: refreshCookieName, Value: "r1"})
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-value"})
	req.Header.Set(csrfHeaderName, "csrf-value")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if rotator.logouts != 1 {
		t.Errorf("logout called %d times", rotator.logouts)
	}
	refresh := cookieNamed(rec, refreshCookieName)
	if refresh == nil || refresh.MaxAge >= 0 {
		t.Error("the refresh cookie was not expired")
	}
}

func TestCookieIsSecureByDefault(t *testing.T) {
	settings := cookieSettings{allowedOrigins: []string{panelOrigin}, secure: true}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Origin", panelOrigin)

	if !settings.isBrowser(req) {
		t.Fatal("the panel origin was not recognised as a browser")
	}
	if err := settings.setSession(rec, session.Tokens{RefreshToken: "r", RefreshExpiresIn: 60}); err != nil {
		t.Fatalf("setSession returned error: %v", err)
	}
	if cookie := cookieNamed(rec, refreshCookieName); cookie == nil || !cookie.Secure {
		t.Error("the refresh cookie is not Secure")
	}
}
