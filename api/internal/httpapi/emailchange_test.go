package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/emailchange"
	"github.com/ivvanare/ecosystem_complete/api/internal/security"
)

type fakeEmailChange struct {
	err    error
	args   []string
	verify []string
}

func (f *fakeEmailChange) Request(_ context.Context, userID, newEmail, password string) error {
	f.args = []string{userID, newEmail, password}
	return f.err
}

func (f *fakeEmailChange) Verify(_ context.Context, userID, code string) error {
	f.verify = []string{userID, code}
	return f.err
}

func emailHandler(t *testing.T, service EmailChangeService) http.Handler {
	t.Helper()
	parser := &fakeParser{}
	parser.claims.Subject = "user-1"

	return New(Options{EmailChange: service, TokenParser: parser, Logger: slog.New(slog.DiscardHandler)})
}

func authPost(t *testing.T, handler http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer x")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestEmailChangeRequestAnswers202(t *testing.T) {
	service := &fakeEmailChange{}
	rec := authPost(t, emailHandler(t, service), "/v1/auth/email/change",
		`{"new_email":"nueva@melia.test","password":"una-clave-larga-123"}`)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body.String())
	}
	if len(service.args) != 3 || service.args[1] != "nueva@melia.test" {
		t.Errorf("service received %v", service.args)
	}
}

func TestEmailChangeMapsItsErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code int
		body string
	}{
		{"quota", &security.TooManyRequestsError{RetryAfter: 30 * time.Second}, http.StatusTooManyRequests, "rate_limited"},
		{"same email", emailchange.ErrSameEmail, http.StatusUnprocessableEntity, "unchanged"},
		{"taken", emailchange.ErrEmailTaken, http.StatusConflict, "email_unavailable"},
		{"wrong password", emailchange.ErrInvalidPassword, http.StatusBadRequest, "invalid_password"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := authPost(t, emailHandler(t, &fakeEmailChange{err: tc.err}), "/v1/auth/email/change",
				`{"new_email":"nueva@melia.test","password":"x"}`)

			if rec.Code != tc.code || !strings.Contains(rec.Body.String(), tc.body) {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestEmailChangeVerifyMapsTheCodeErrors(t *testing.T) {
	rec := authPost(t, emailHandler(t, &fakeEmailChange{err: emailchange.ErrInvalidCode}),
		"/v1/auth/email/change/verify", `{"code":"042317"}`)

	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_code") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
