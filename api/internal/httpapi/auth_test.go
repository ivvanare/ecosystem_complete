package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/login"
	"github.com/ivvanare/ecosystem_complete/api/internal/session"
	"github.com/ivvanare/ecosystem_complete/api/internal/signup"
)

type fakeSignup struct {
	calls   int
	email   string
	name    string
	pass    string
	wantErr error
}

func (f *fakeSignup) Request(_ context.Context, email, name, password string) error {
	f.calls++
	f.email, f.name, f.pass = email, name, password
	return f.wantErr
}

func newAuthHandler(t *testing.T, signup SignupRequester) http.Handler {
	t.Helper()
	return New(Options{Signup: signup, Logger: slog.New(slog.DiscardHandler)})
}

func postSignup(t *testing.T, handler http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestSignupAcceptsAValidRequest(t *testing.T) {
	signup := &fakeSignup{}
	rec := postSignup(t, newAuthHandler(t, signup),
		`{"email":"ana@melia.test","name":"Ana","password":"una-clave-larga-123"}`)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty: the response must not differ by account state", rec.Body.String())
	}
	if signup.calls != 1 {
		t.Fatalf("service called %d times, want 1", signup.calls)
	}
	if signup.email != "ana@melia.test" || signup.name != "Ana" || signup.pass != "una-clave-larga-123" {
		t.Errorf("service received %q/%q/%q", signup.email, signup.name, signup.pass)
	}
}

func TestSignupRejectsInvalidFieldsWithTheEnvelope(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		field string
		code  string
	}{
		{"bad email", `{"email":"no-es-email","name":"Ana","password":"una-clave-larga-123"}`, "email", "invalid"},
		{"empty name", `{"email":"ana@melia.test","name":"  ","password":"una-clave-larga-123"}`, "name", "required"},
		{"short password", `{"email":"ana@melia.test","name":"Ana","password":"corta"}`, "password", "too_short"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			signup := &fakeSignup{}
			rec := postSignup(t, newAuthHandler(t, signup), tc.body)

			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", rec.Code)
			}
			if signup.calls != 0 {
				t.Errorf("service was called for an invalid request")
			}

			var body struct {
				Error struct {
					Code    string `json:"code"`
					Details struct {
						Fields []fieldError `json:"fields"`
					} `json:"details"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("body is not JSON: %v", err)
			}
			if body.Error.Code != "validation_failed" {
				t.Errorf("code = %q, want validation_failed", body.Error.Code)
			}
			found := false
			for _, f := range body.Error.Details.Fields {
				if f.Field == tc.field && f.Code == tc.code {
					found = true
				}
			}
			if !found {
				t.Errorf("fields = %+v, want %s/%s", body.Error.Details.Fields, tc.field, tc.code)
			}
		})
	}
}

func TestSignupRejectsMalformedJSON(t *testing.T) {
	rec := postSignup(t, newAuthHandler(t, &fakeSignup{}), `{`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestSignupHidesServiceFailuresBehindAGenericError(t *testing.T) {
	signup := &fakeSignup{wantErr: errors.New("queue: dialing broker: connection refused")}
	rec := postSignup(t, newAuthHandler(t, signup),
		`{"email":"ana@melia.test","name":"Ana","password":"una-clave-larga-123"}`)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "broker") {
		t.Errorf("response leaks the internal error: %s", rec.Body.String())
	}
}

type fakeVerifier struct {
	tokens session.Tokens
	err    error
	email  string
	code   string
	calls  int
}

func (f *fakeVerifier) Verify(_ context.Context, email, code string, _ session.Meta) (session.Tokens, error) {
	f.calls++
	f.email, f.code = email, code
	return f.tokens, f.err
}

func postVerify(t *testing.T, handler http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/signup/verify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestVerifySignupReturnsTheTokenPair(t *testing.T) {
	verifier := &fakeVerifier{tokens: session.Tokens{
		AccessToken: "access", ExpiresIn: 900, RefreshToken: "refresh", RefreshExpiresIn: 2592000,
	}}
	// Signup is nil on purpose: verification must work while the broker is down.
	handler := New(Options{SignupVerifier: verifier, Logger: slog.New(slog.DiscardHandler)})

	rec := postVerify(t, handler, `{"email":"ana@melia.test","code":"042317"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}

	var body tokenPairResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if body.AccessToken != "access" || body.RefreshToken != "refresh" || body.TokenType != "Bearer" {
		t.Errorf("unexpected pair: %+v", body)
	}
	if verifier.email != "ana@melia.test" || verifier.code != "042317" {
		t.Errorf("verifier received %q/%q", verifier.email, verifier.code)
	}
}

func TestVerifySignupMapsContractErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code int
		body string
	}{
		{"wrong or expired code", signup.ErrInvalidCode, http.StatusBadRequest, "invalid_code"},
		{"counter spent", signup.ErrTooManyAttempts, http.StatusTooManyRequests, "too_many_attempts"},
		{"internal", errors.New("db down"), http.StatusInternalServerError, "internal_error"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := New(Options{
				SignupVerifier: &fakeVerifier{err: tc.err},
				Logger:         slog.New(slog.DiscardHandler),
			})
			rec := postVerify(t, handler, `{"email":"ana@melia.test","code":"042317"}`)

			if rec.Code != tc.code {
				t.Fatalf("status = %d, want %d", rec.Code, tc.code)
			}
			if !strings.Contains(rec.Body.String(), tc.body) {
				t.Errorf("body %s does not carry %q", rec.Body.String(), tc.body)
			}
		})
	}
}

func TestVerifySignupRejectsAMalformedCodeBeforeCallingTheService(t *testing.T) {
	verifier := &fakeVerifier{}
	handler := New(Options{SignupVerifier: verifier, Logger: slog.New(slog.DiscardHandler)})

	rec := postVerify(t, handler, `{"email":"ana@melia.test","code":"12ab"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if verifier.calls != 0 {
		t.Error("the service was called with a malformed code")
	}
}

type fakeLogin struct {
	tokens session.Tokens
	err    error
	email  string
	device string
	calls  int
}

func (f *fakeLogin) Login(_ context.Context, email, _ string, meta session.Meta) (session.Tokens, error) {
	f.calls++
	f.email, f.device = email, meta.Device
	return f.tokens, f.err
}

func postLogin(t *testing.T, handler http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestLoginReturnsTheTokenPair(t *testing.T) {
	auth := &fakeLogin{tokens: session.Tokens{AccessToken: "a", ExpiresIn: 900, RefreshToken: "r", RefreshExpiresIn: 100}}
	handler := New(Options{Login: auth, Logger: slog.New(slog.DiscardHandler)})

	rec := postLogin(t, handler, `{"email":"ana@melia.test","password":"una-clave-larga-123","device":"iPhone"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"token_type":"Bearer"`) {
		t.Errorf("body = %s", rec.Body.String())
	}
	if auth.email != "ana@melia.test" || auth.device != "iPhone" {
		t.Errorf("service received %q/%q", auth.email, auth.device)
	}
}

func TestLoginMapsFailures(t *testing.T) {
	t.Run("invalid credentials", func(t *testing.T) {
		handler := New(Options{Login: &fakeLogin{err: login.ErrInvalidCredentials}, Logger: slog.New(slog.DiscardHandler)})
		rec := postLogin(t, handler, `{"email":"ana@melia.test","password":"x"}`)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "invalid_credentials") {
			t.Errorf("body = %s", rec.Body.String())
		}
	})

	t.Run("locked account", func(t *testing.T) {
		handler := New(Options{
			Login:  &fakeLogin{err: &login.LockedError{RetryAfter: 90 * time.Second}},
			Logger: slog.New(slog.DiscardHandler),
		})
		rec := postLogin(t, handler, `{"email":"ana@melia.test","password":"x"}`)

		if rec.Code != http.StatusLocked {
			t.Fatalf("status = %d, want 423", rec.Code)
		}
		if got := rec.Header().Get("Retry-After"); got != "90" {
			t.Errorf("Retry-After = %q, want 90", got)
		}
		if !strings.Contains(rec.Body.String(), "retry_after_seconds") {
			t.Errorf("body = %s", rec.Body.String())
		}
	})

	t.Run("validation", func(t *testing.T) {
		auth := &fakeLogin{}
		handler := New(Options{Login: auth, Logger: slog.New(slog.DiscardHandler)})
		rec := postLogin(t, handler, `{"email":"no-es-email","password":""}`)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422", rec.Code)
		}
		if auth.calls != 0 {
			t.Error("the service was called with invalid input")
		}
	})
}
