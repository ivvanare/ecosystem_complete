package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/ivvanare/ecosystem_complete/api/internal/identity"
)

// fakeRoles scripts the role-authoring surface.
type fakeRoles struct {
	role     identity.Role
	err      error
	calls    int
	gotIn    identity.RoleInput
	gotUp    identity.RoleUpdate
	gotID    string
	gotCodes []string
}

func (f *fakeRoles) CreateRole(_ context.Context, _ string, in identity.RoleInput) (identity.Role, error) {
	f.calls++
	f.gotIn = in
	return f.role, f.err
}

func (f *fakeRoles) Role(_ context.Context, roleID string) (identity.Role, error) {
	f.calls++
	f.gotID = roleID
	return f.role, f.err
}

func (f *fakeRoles) UpdateRole(_ context.Context, _, roleID string, in identity.RoleUpdate) (identity.Role, error) {
	f.calls++
	f.gotID, f.gotUp = roleID, in
	return f.role, f.err
}

func (f *fakeRoles) RetireRole(_ context.Context, _, roleID string) error {
	f.calls++
	f.gotID = roleID
	return f.err
}

func (f *fakeRoles) ReplaceRolePermissions(_ context.Context, _, roleID string, codes []string) ([]string, error) {
	f.calls++
	f.gotID, f.gotCodes = roleID, codes
	return codes, f.err
}

// roleScope is the seeded SUPERADMIN: admin.access AND role.manage.
var roleScope = grantedPermissions{granted: map[string]bool{
	AdminPermission: true, RoleManagePermission: true,
}}

func roleRouter(service RoleAdmin) http.Handler {
	return New(Options{
		RoleAdmin:   service,
		TokenParser: authedParser("root"),
		Authorizer:  roleScope,
		Logger:      slog.New(slog.DiscardHandler),
	})
}

const rolePath = "/v1/admin/roles"

func TestCreateRoleAnswers201WithItsPermissions(t *testing.T) {
	service := &fakeRoles{role: identity.Role{ID: adminPlaceID, Name: "OPERADOR",
		Status: "ACTIVE", Permissions: []string{"admin.access"}}}
	rec := bearerDo(t, roleRouter(service), http.MethodPost, rolePath,
		`{"name":"OPERADOR","permissions":["admin.access"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d (%s), want 201", rec.Code, rec.Body.String())
	}
	if service.gotIn.Name != "OPERADOR" || len(service.gotIn.Permissions) != 1 {
		t.Fatalf("service got %+v", service.gotIn)
	}
	if !strings.Contains(rec.Body.String(), `"permissions":["admin.access"]`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

// Authoring a role is role.manage, not admin.access: an account that may enter the backoffice but
// may not change roles must be refused here, and the seeded ADMIN role is exactly that account.
func TestRoleAuthoringRequiresRoleManage(t *testing.T) {
	service := &fakeRoles{}
	rec := bearerDo(t, New(Options{
		RoleAdmin:   service,
		TokenParser: authedParser("admin-user"),
		Authorizer:  adminScope, // admin.access only
		Logger:      slog.New(slog.DiscardHandler),
	}), http.MethodPost, rolePath, `{"name":"X"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (%s)", rec.Code, rec.Body.String())
	}
	if service.calls != 0 {
		t.Fatal("the service was reached without role.manage")
	}
}

func TestRetireRoleInUseAnswers409WithTheCount(t *testing.T) {
	service := &fakeRoles{err: &identity.RoleInUseError{Users: 3}}
	rec := bearerDo(t, roleRouter(service), http.MethodDelete, rolePath+"/"+adminPlaceID, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"role_in_use"`) || !strings.Contains(rec.Body.String(), `"users":3`) {
		t.Fatalf("body = %s, want role_in_use and the count", rec.Body.String())
	}
}

func TestTheSystemRoleRefusalIsItsOwnCode(t *testing.T) {
	service := &fakeRoles{err: identity.ErrRoleProtected}
	for _, call := range []struct {
		method, path, body string
	}{
		{http.MethodDelete, rolePath + "/" + adminPlaceID, ""},
		{http.MethodPatch, rolePath + "/" + adminPlaceID, `{"name":"OTRO"}`},
		{http.MethodPut, rolePath + "/" + adminPlaceID + "/permissions", `{"codes":[]}`},
	} {
		rec := bearerDo(t, roleRouter(service), call.method, call.path, call.body)
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"role_protected"`) {
			t.Fatalf("%s %s: status = %d (%s), want 409 role_protected",
				call.method, call.path, rec.Code, rec.Body.String())
		}
	}
}

func TestADuplicateRoleNameNamesTheField(t *testing.T) {
	service := &fakeRoles{err: identity.ErrRoleNameTaken}
	rec := bearerDo(t, roleRouter(service), http.MethodPost, rolePath, `{"name":"ADMIN"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"field":"name","code":"duplicate"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestAnUnknownPermissionNamesTheField(t *testing.T) {
	service := &fakeRoles{err: &identity.UnknownPermissionError{Codes: []string{"nope"}}}
	rec := bearerDo(t, roleRouter(service), http.MethodPut, rolePath+"/"+adminPlaceID+"/permissions",
		`{"codes":["nope"]}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"field":"permissions","code":"unknown"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestRoleShapesAreFieldErrors(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{identity.ErrRoleNameRequired, `"field":"name","code":"required"`},
		{identity.ErrRoleNameTooLong, `"field":"name","code":"too_long"`},
		{identity.ErrInvalidRoleStatus, `"field":"status","code":"invalid"`},
		{identity.ErrInvalidRolePermissions, `"field":"permissions","code":"invalid"`},
	}
	for _, tc := range cases {
		rec := bearerDo(t, roleRouter(&fakeRoles{err: tc.err}), http.MethodPatch,
			rolePath+"/"+adminPlaceID, `{"name":"X"}`)
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), tc.want) {
			t.Fatalf("%v: status = %d (%s), want 422 %s", tc.err, rec.Code, rec.Body.String(), tc.want)
		}
	}
}

func TestAMissingRoleIs404(t *testing.T) {
	rec := bearerDo(t, roleRouter(&fakeRoles{err: identity.ErrRoleNotFound}), http.MethodGet,
		rolePath+"/"+adminPlaceID, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestAMalformedRoleIDIs422(t *testing.T) {
	service := &fakeRoles{}
	rec := bearerDo(t, roleRouter(service), http.MethodGet, rolePath+"/nope", "")
	if rec.Code != http.StatusUnprocessableEntity || service.calls != 0 {
		t.Fatalf("status = %d, calls = %d", rec.Code, service.calls)
	}
}

func TestAnUnexpectedFailureIs500(t *testing.T) {
	rec := bearerDo(t, roleRouter(&fakeRoles{err: errors.New("db down")}), http.MethodGet,
		rolePath+"/"+adminPlaceID, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

// The contract declares `codes` required, and a body without it must not empty a role: the same
// omission on the user-roles route already answers 422 required.
func TestReplacePermissionsRefusesABodyWithoutCodes(t *testing.T) {
	for _, body := range []string{`{}`, `{"codes":null}`} {
		service := &fakeRoles{role: identity.Role{ID: adminPlaceID, Name: "OPERADOR", Status: "ACTIVE"}}
		rec := bearerDo(t, roleRouter(service), http.MethodPut, rolePath+"/"+adminPlaceID+"/permissions", body)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s: status = %d (%s), want 422", body, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"field":"codes","code":"required"`) {
			t.Fatalf("%s: body = %s", body, rec.Body.String())
		}
		if service.calls != 0 {
			t.Fatalf("%s: the domain was asked to wipe the set", body)
		}
	}
}

// RoleUpdate declares `description: [string, null]`, so an explicit null clears it. A Go **string
// cannot carry that through encoding/json: null and absent both land as a nil outer pointer.
func TestUpdateRoleClearsAnExplicitNullDescription(t *testing.T) {
	service := &fakeRoles{role: identity.Role{ID: adminPlaceID, Name: "OPERADOR", Status: "ACTIVE"}}
	rec := bearerDo(t, roleRouter(service), http.MethodPatch, rolePath+"/"+adminPlaceID, `{"description":null}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	if service.gotUp.Description == nil {
		t.Fatal(`{"description":null} arrived as "leave it alone"`)
	}
	if *service.gotUp.Description != nil {
		t.Fatalf("description = %q, want cleared", **service.gotUp.Description)
	}
}

// And an omitted description must still mean "leave it alone".
func TestUpdateRoleLeavesAnOmittedDescriptionAlone(t *testing.T) {
	service := &fakeRoles{role: identity.Role{ID: adminPlaceID, Name: "OPERADOR", Status: "ACTIVE"}}
	rec := bearerDo(t, roleRouter(service), http.MethodPatch, rolePath+"/"+adminPlaceID, `{"name":"OTRO"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	if service.gotUp.Description != nil {
		t.Fatal("an omitted description was treated as a change")
	}
}
