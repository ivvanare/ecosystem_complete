package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ivvanare/ecosystem_complete/api/internal/session"
)

type fakeParser struct {
	claims session.Claims
	err    error
}

func (f *fakeParser) Parse(string) (session.Claims, error) {
	if f.err != nil {
		return session.Claims{}, f.err
	}
	return f.claims, nil
}

type fakeAuthorizer struct {
	allowed bool
	err     error
	calls   int
}

func (f *fakeAuthorizer) Can(context.Context, string, string) (bool, error) {
	f.calls++
	return f.allowed, f.err
}

func okHandler(t *testing.T, wantUser string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := userIDFrom(r.Context())
		if !ok || userID != wantUser {
			t.Errorf("handler saw user %q (ok=%v), want %q", userID, ok, wantUser)
		}
		w.WriteHeader(http.StatusOK)
	}
}

func TestRequireAuth(t *testing.T) {
	valid := &fakeParser{}

	cases := []struct {
		name   string
		header string
		parser *fakeParser
		status int
	}{
		{"no header", "", valid, http.StatusUnauthorized},
		{"wrong scheme", "Basic abc", valid, http.StatusUnauthorized},
		{"empty token", "Bearer ", valid, http.StatusUnauthorized},
		{"invalid token", "Bearer x", &fakeParser{err: session.ErrInvalidToken}, http.StatusUnauthorized},
		{"valid token", "Bearer x", valid, http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := RequireAuth(tc.parser, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			handler(rec, req)

			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
		})
	}
}

func TestRequireAuthPassesTheSubject(t *testing.T) {
	parser := &fakeParser{}
	parser.claims.Subject = "user-1"

	handler := RequireAuth(parser, okHandler(t, "user-1"))
	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Bearer x")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestRequirePermission(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)

	cases := []struct {
		name       string
		authorizer *fakeAuthorizer
		status     int
	}{
		{"allowed", &fakeAuthorizer{allowed: true}, http.StatusOK},
		{"denied", &fakeAuthorizer{allowed: false}, http.StatusForbidden},
		{"lookup failed", &fakeAuthorizer{err: errors.New("db down")}, http.StatusInternalServerError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := RequirePermission(tc.authorizer, "admin.access", logger, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/v1/admin/roles", nil)
			ctx := context.WithValue(req.Context(), userIDKey, "user-1")
			rec := httptest.NewRecorder()
			handler(rec, req.WithContext(ctx))

			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
		})
	}
}

func TestRequirePermissionWithoutAUserIsUnauthorized(t *testing.T) {
	handler := RequirePermission(&fakeAuthorizer{allowed: true}, "admin.access", slog.New(slog.DiscardHandler),
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/v1/admin/roles", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
