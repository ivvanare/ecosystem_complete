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
