package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ivvanare/ecosystem_complete/api/internal/password"
)

type fakePassword struct {
	requested string
	resetArgs []string
	changeArg []string
	err       error
}

func (f *fakePassword) RequestReset(_ context.Context, email string) error {
	f.requested = email
	return f.err
}

func (f *fakePassword) Reset(_ context.Context, email, code, newPassword string) error {
	f.resetArgs = []string{email, code, newPassword}
	return f.err
}

func (f *fakePassword) Change(_ context.Context, userID, current, next string) error {
	f.changeArg = []string{userID, current, next}
	return f.err
}

func passwordHandler(t *testing.T, service PasswordService) http.Handler {
	t.Helper()
	parser := &fakeParser{}
	parser.claims.Subject = "user-1"

	return New(Options{
		Password:    service,
		TokenParser: parser,
		Logger:      slog.New(slog.DiscardHandler),
	})
}

func TestForgotAlwaysAnswers202(t *testing.T) {
	service := &fakePassword{}
	handler := passwordHandler(t, service)

	rec := postJSONTo(t, handler, "/v1/auth/password/forgot", `{"email":"ana@melia.test"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", rec.Body.String())
	}
	if service.requested != "ana@melia.test" {
		t.Errorf("service received %q", service.requested)
	}

	// Same answer for a malformed address? No: that is a client error, not an oracle.
	bad := postJSONTo(t, handler, "/v1/auth/password/forgot", `{"email":"no-es-email"}`)
	if bad.Code != http.StatusUnprocessableEntity {
		t.Errorf("malformed email status = %d, want 422", bad.Code)
	}
}

func TestResetMapsTheCodeErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code int
		body string
	}{
		{"wrong code", password.ErrInvalidCode, http.StatusBadRequest, "invalid_code"},
		{"spent counter", password.ErrTooManyAttempts, http.StatusTooManyRequests, "too_many_attempts"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := passwordHandler(t, &fakePassword{err: tc.err})
			rec := postJSONTo(t, handler, "/v1/auth/password/reset",
				`{"email":"ana@melia.test","code":"042317","new_password":"nueva-clave-larga-123"}`)

			if rec.Code != tc.code || !strings.Contains(rec.Body.String(), tc.body) {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestResetValidatesTheNewPassword(t *testing.T) {
	service := &fakePassword{}
	handler := passwordHandler(t, service)

	rec := postJSONTo(t, handler, "/v1/auth/password/reset",
		`{"email":"ana@melia.test","code":"042317","new_password":"corta"}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if len(service.resetArgs) != 0 {
		t.Error("the service was called with a password that is too short")
	}
}

func TestChangeRequiresATokenAndMapsAWrongPassword(t *testing.T) {
	service := &fakePassword{err: password.ErrInvalidCurrentPassword}
	handler := passwordHandler(t, service)

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/password/change",
		strings.NewReader(`{"current_password":"x","new_password":"nueva-clave-larga-123"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 without a bearer token", rec.Code)
	}

	req.Header.Set("Authorization", "Bearer x")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_password") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(service.changeArg) != 3 || service.changeArg[0] != "user-1" {
		t.Errorf("service received %v", service.changeArg)
	}
}
