package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/ivvanare/ecosystem_complete/api/internal/session"
	"github.com/ivvanare/ecosystem_complete/api/internal/social"
)

type fakeSocial struct {
	tokens   session.Tokens
	err      error
	provider string
	calls    int
}

func (f *fakeSocial) Authenticate(_ context.Context, provider, _ string, _ session.Meta) (session.Tokens, error) {
	f.calls++
	f.provider = provider
	return f.tokens, f.err
}

func socialHandler(t *testing.T, service SocialAuthenticator) http.Handler {
	t.Helper()
	return New(Options{Social: service, Logger: slog.New(slog.DiscardHandler)})
}

func TestSocialLoginReturnsTheTokenPair(t *testing.T) {
	service := &fakeSocial{tokens: session.Tokens{AccessToken: "a", RefreshToken: "r", ExpiresIn: 900}}
	handler := socialHandler(t, service)

	rec := postJSONTo(t, handler, "/v1/auth/social", `{"provider":"google","id_token":"id-token"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if service.provider != social.ProviderGoogle {
		t.Errorf("provider = %q, want normalized to GOOGLE", service.provider)
	}
	if !strings.Contains(rec.Body.String(), `"refresh_token":"r"`) {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestSocialLoginMapsFailures(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code int
		body string
	}{
		{"invalid token", social.ErrInvalidToken, http.StatusUnauthorized, "invalid_token"},
		{"unconfigured provider", social.ErrUnknownProvider, http.StatusUnprocessableEntity, "validation_failed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := postJSONTo(t, socialHandler(t, &fakeSocial{err: tc.err}), "/v1/auth/social",
				`{"provider":"APPLE","id_token":"x"}`)

			if rec.Code != tc.code || !strings.Contains(rec.Body.String(), tc.body) {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestSocialLoginValidatesTheRequest(t *testing.T) {
	service := &fakeSocial{}
	handler := socialHandler(t, service)

	for _, body := range []string{`{"provider":"FACEBOOK","id_token":"x"}`, `{"provider":"GOOGLE"}`} {
		rec := postJSONTo(t, handler, "/v1/auth/social", body)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("body %s: status = %d, want 422", body, rec.Code)
		}
	}
	if service.calls != 0 {
		t.Error("the service was called with an invalid request")
	}
}
