package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/audit"
	"github.com/ivvanare/ecosystem_complete/api/internal/identity"
)

type fakeAdminReader struct {
	users     identity.Page[identity.UserSummary]
	user      identity.UserSummary
	setErr    error
	events    identity.Page[identity.AuditEvent]
	gotRoles  []string
	gotFilter identity.UserFilter
	// Captured profile edit, so a dropped field fails the test.
	gotName   *string
	gotStatus *string
	updateErr error
	updated   identity.UserSummary
}

func (f *fakeAdminReader) Users(_ context.Context, filter identity.UserFilter) (identity.Page[identity.UserSummary], error) {
	f.gotFilter = filter
	return f.users, nil
}

func (f *fakeAdminReader) User(context.Context, string) (identity.UserSummary, error) {
	return f.user, nil
}

func (f *fakeAdminReader) UpdateUser(_ context.Context, _, _ string, name, status *string, _ audit.Event) (identity.UserSummary, error) {
	f.gotName, f.gotStatus = name, status
	if f.updateErr != nil {
		return identity.UserSummary{}, f.updateErr
	}
	return f.updated, nil
}

func (f *fakeAdminReader) SetRoles(_ context.Context, _, _ string, roles []string, _ audit.Event) ([]string, error) {
	f.gotRoles = roles
	return roles, f.setErr
}

func (f *fakeAdminReader) AuditEvents(context.Context, identity.AuditFilter) (identity.Page[identity.AuditEvent], error) {
	return f.events, nil
}

func adminHandler(t *testing.T, reader AdminReader) http.Handler {
	t.Helper()
	parser := &fakeParser{}
	parser.claims.Subject = "admin-1"

	return New(Options{
		TokenParser: parser,
		Authorizer:  &fakeAuthorizer{allowed: true},
		Identity:    &fakeIdentity{},
		Admin:       reader,
		Logger:      slog.New(slog.DiscardHandler),
	})
}

func adminRequest(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer x")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestAdminUsersListsWithContractPagination(t *testing.T) {
	name, email := "Ana", "ana@melia.test"
	reader := &fakeAdminReader{users: identity.Page[identity.UserSummary]{
		Data:  []identity.UserSummary{{ID: "user-1", Name: &name, Email: &email, Status: "ACTIVE", Roles: []string{"EDITOR"}}},
		Page:  1,
		Limit: 20,
		Total: 1,
	}}

	rec := adminRequest(t, adminHandler(t, reader), http.MethodGet, "/v1/admin/users?q=ana", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Data  []userSummaryResponse `json:"data"`
		Page  int                   `json:"page"`
		Limit int                   `json:"limit"`
		Total int                   `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if body.Page != 1 || body.Limit != 20 || body.Total != 1 || len(body.Data) != 1 {
		t.Errorf("unexpected page: %+v", body)
	}
	if reader.gotFilter.Query != "ana" {
		t.Errorf("filter q = %q", reader.gotFilter.Query)
	}
}

func TestAdminUsersClampsTheLimit(t *testing.T) {
	reader := &fakeAdminReader{}
	adminRequest(t, adminHandler(t, reader), http.MethodGet, "/v1/admin/users?limit=9999", "")

	if reader.gotFilter.Limit != 100 {
		t.Errorf("limit = %d, want 100", reader.gotFilter.Limit)
	}
}

func TestSetRolesReplacesAndReports(t *testing.T) {
	reader := &fakeAdminReader{user: identity.UserSummary{ID: "user-2", Roles: []string{"ADMIN"}}}

	rec := adminRequest(t, adminHandler(t, reader), http.MethodPatch, "/v1/admin/users/user-2/roles", `{"roles":["ADMIN"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(reader.gotRoles) != 1 || reader.gotRoles[0] != "ADMIN" {
		t.Errorf("roles sent = %v", reader.gotRoles)
	}
	if !strings.Contains(rec.Body.String(), `"roles":["ADMIN"]`) {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestSetRolesRejectsUnknownRoles(t *testing.T) {
	reader := &fakeAdminReader{setErr: identity.ErrUnknownRole}

	rec := adminRequest(t, adminHandler(t, reader), http.MethodPatch, "/v1/admin/users/user-2/roles", `{"roles":["NADA"]}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"unknown"`) {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestSetRolesRequiresTheRolesField(t *testing.T) {
	reader := &fakeAdminReader{}
	rec := adminRequest(t, adminHandler(t, reader), http.MethodPatch, "/v1/admin/users/user-2/roles", `{}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if reader.gotRoles != nil {
		t.Error("the service was called without a roles field")
	}
}

func TestAdminAuditEventsLists(t *testing.T) {
	event := "auth.login.failed"
	ip := "10.0.0.1"
	reader := &fakeAdminReader{events: identity.Page[identity.AuditEvent]{
		Data: []identity.AuditEvent{{
			ID: "evt-1", Event: event, IP: &ip,
			Metadata:  map[string]any{"reason": "wrong_password"},
			CreatedAt: time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC),
		}},
		Page: 1, Limit: 20, Total: 1,
	}}

	rec := adminRequest(t, adminHandler(t, reader), http.MethodGet, "/v1/admin/audit-events?event=auth.login.failed", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"event":"auth.login.failed"`) {
		t.Errorf("body = %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"reason":"wrong_password"`) {
		t.Errorf("metadata lost: %s", rec.Body.String())
	}
}

func TestAdminAuditEventsRejectsABadDate(t *testing.T) {
	rec := adminRequest(t, adminHandler(t, &fakeAdminReader{}), http.MethodGet, "/v1/admin/audit-events?from=ayer", "")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
}

func TestAdminUsersScopeFilter(t *testing.T) {
	reader := &fakeAdminReader{}
	adminRequest(t, adminHandler(t, reader), http.MethodGet, "/v1/admin/users?scope=admin", "")
	if reader.gotFilter.Scope != "admin" {
		t.Errorf("scope = %q, want it forwarded", reader.gotFilter.Scope)
	}
}

func TestAdminUsersRejectsAnUnknownScope(t *testing.T) {
	rec := adminRequest(t, adminHandler(t, &fakeAdminReader{}), http.MethodGet, "/v1/admin/users?scope=root", "")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
}

func TestAdminUpdateUserEditsNameAndStatus(t *testing.T) {
	name, status := "Ana María", "SUSPENDED"
	reader := &fakeAdminReader{updated: identity.UserSummary{ID: "u1", Name: &name, Status: status, Roles: []string{}}}

	rec := adminRequest(t, adminHandler(t, reader), http.MethodPatch, "/v1/admin/users/u1",
		`{"name":"  Ana María  ","status":"SUSPENDED"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if reader.gotName == nil || *reader.gotName != "Ana María" {
		t.Errorf("name = %v, want it trimmed and forwarded", reader.gotName)
	}
	if reader.gotStatus == nil || *reader.gotStatus != "SUSPENDED" {
		t.Errorf("status = %v", reader.gotStatus)
	}
	if !strings.Contains(rec.Body.String(), `"name":"Ana María"`) {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestAdminUpdateUserRejectsAnUnknownStatus(t *testing.T) {
	rec := adminRequest(t, adminHandler(t, &fakeAdminReader{}), http.MethodPatch, "/v1/admin/users/u1",
		`{"status":"DELETED"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
}

func TestAdminUpdateUserRequiresAField(t *testing.T) {
	rec := adminRequest(t, adminHandler(t, &fakeAdminReader{}), http.MethodPatch, "/v1/admin/users/u1", `{}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
}

func TestAdminUpdateUserReportsAnUnknownUser(t *testing.T) {
	reader := &fakeAdminReader{updateErr: identity.ErrNotFound}
	rec := adminRequest(t, adminHandler(t, reader), http.MethodPatch, "/v1/admin/users/nope", `{"name":"X"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
