package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/ivvanare/ecosystem_complete/api/internal/placeadmin"
)

// fakePlaceAdmin is a scriptable PlaceBackoffice that records the viewer it was asked with.
type fakePlaceAdmin struct {
	page         placeadmin.Page
	place        placeadmin.Place
	err          error
	gotViewer    placeadmin.Viewer
	gotFilter    placeadmin.Filter
	gotInput     placeadmin.CreateInput
	gotUpdate    placeadmin.UpdateInput
	calls        int
	createdFor   string
	categories   []placeadmin.Category
	schedules    []placeadmin.Schedule
	attributes   []string
	gotSchedules []placeadmin.Schedule
	gotCodes     []string
	gotStatus    string
}

func (f *fakePlaceAdmin) List(_ context.Context, viewer placeadmin.Viewer, filter placeadmin.Filter, _, _ int) (placeadmin.Page, error) {
	f.calls++
	f.gotViewer, f.gotFilter = viewer, filter
	return f.page, f.err
}

func (f *fakePlaceAdmin) Get(_ context.Context, viewer placeadmin.Viewer, _ string) (placeadmin.Place, error) {
	f.calls++
	f.gotViewer = viewer
	return f.place, f.err
}

func (f *fakePlaceAdmin) Create(_ context.Context, actorID string, in placeadmin.CreateInput) (placeadmin.Place, error) {
	f.calls++
	f.createdFor, f.gotInput = actorID, in
	return f.place, f.err
}

func (f *fakePlaceAdmin) Update(_ context.Context, viewer placeadmin.Viewer, _, _ string, in placeadmin.UpdateInput) (placeadmin.Place, error) {
	f.calls++
	f.gotViewer, f.gotUpdate = viewer, in
	return f.place, f.err
}

func (f *fakePlaceAdmin) Categories(_ context.Context, status string, _, _ int) ([]placeadmin.Category, int, error) {
	f.calls++
	f.gotStatus = status
	return f.categories, len(f.categories), f.err
}

func (f *fakePlaceAdmin) Retire(_ context.Context, viewer placeadmin.Viewer, _, _ string) error {
	f.calls++
	f.gotViewer = viewer
	return f.err
}

const adminPlaceID = "0b1c2d3e-0000-4000-8000-000000000004"

// grantedPermissions answers per permission code. A single boolean fake cannot express the
// case this slice is about: an owner-editor holds place.update but NOT admin.access.
type grantedPermissions struct{ granted map[string]bool }

func (g grantedPermissions) Can(_ context.Context, _, permission string) (bool, error) {
	return g.granted[permission], nil
}

// adminScope is the backoffice operator: the whole catalogue.
var adminScope = grantedPermissions{granted: map[string]bool{
	AdminPermission: true, PlaceManageAllPermission: true, PlaceCreatePermission: true,
	PlaceUpdatePermission: true, PlaceDeletePermission: true,
}}

// ownerScope mirrors the real seeded EDITOR role, which HOLDS admin.access (it means "may
// enter the backoffice") but not place.manage_all. That is the case the scoping exists for:
// an editor must not see the whole catalogue just because it can open the panel.
var ownerScope = grantedPermissions{granted: map[string]bool{
	AdminPermission:       true,
	PlaceUpdatePermission: true, PlaceDeletePermission: true,
}}

// backoffice builds a handler whose caller is the given user with the given permissions.
func backoffice(service PlaceBackoffice, userID string, scope grantedPermissions) http.Handler {
	return New(Options{
		PlacesAdmin: service,
		TokenParser: authedParser(userID),
		Authorizer:  scope,
		Logger:      slog.New(slog.DiscardHandler),
	})
}

func TestPlacesBackofficeScopeComesFromThePermission(t *testing.T) {
	// The same call, two callers: an admin manages the whole catalogue, an owner-editor is
	// scoped to their own rows. The viewer is resolved from authorizer, never from the body.
	cases := []struct {
		name       string
		admin      bool
		wantManage bool
	}{
		{"admin", true, true},
		{"owner editor", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service := &fakePlaceAdmin{}
			rec := bearerDo(t, backoffice(service, "user-1", scopeFor(tc.admin)), http.MethodGet, "/v1/admin/places", "")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
			}
			if service.gotViewer.ManageAll != tc.wantManage {
				t.Errorf("ManageAll = %v, want %v", service.gotViewer.ManageAll, tc.wantManage)
			}
			if service.gotViewer.UserID != "user-1" {
				t.Errorf("viewer = %q, want the caller", service.gotViewer.UserID)
			}
		})
	}
}

func TestPlacesBackofficeListPassesFilters(t *testing.T) {
	service := &fakePlaceAdmin{}
	rec := bearerDo(t, backoffice(service, "user-1", adminScope), http.MethodGet,
		"/v1/admin/places?q=arepa&status=INACTIVE&country=VE&region=VE-A&page=2&limit=10", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	filter := service.gotFilter
	if filter.Query != "arepa" || filter.Status != "INACTIVE" || filter.Country != "VE" || filter.Region != "VE-A" {
		t.Errorf("filter = %+v", filter)
	}
}

func TestPlacesBackofficeRejectsBadFiltersBeforeTheService(t *testing.T) {
	cases := []struct{ query, field string }{
		{"?status=GONE", "status"},
		{"?country=ven", "country"},
		{"?region=ve-a", "region"},
		{"?categoryId=nope", "categoryId"},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			service := &fakePlaceAdmin{}
			rec := bearerDo(t, backoffice(service, "user-1", adminScope), http.MethodGet, "/v1/admin/places"+tc.query, "")
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", rec.Code)
			}
			_, fields := errorFields(t, rec)
			if len(fields) == 0 || fields[0].Field != tc.field {
				t.Fatalf("fields = %+v, want %s", fields, tc.field)
			}
			if service.calls != 0 {
				t.Fatal("the service was reached with an invalid filter")
			}
		})
	}
}

func TestPlacesBackofficeCreateRequiresAdminAndAssignsTheOwner(t *testing.T) {
	service := &fakePlaceAdmin{place: placeadmin.Place{ID: adminPlaceID, Name: "N", Title: "T"}}
	body := `{"name":"Arepera","title":"Arepera","coPlaceCategory":"0b1c2d3e-0000-4000-8000-00000000000a",
	          "country":"VE","timezone":"America/Caracas","latitude":10.48,"longitude":-66.9,
	          "coOwnerUser":"0b1c2d3e-0000-4000-8000-000000000001"}`

	// An owner-editor holds place.update but not admin.access: creation is backoffice-only.
	rec := bearerDo(t, backoffice(service, "editor", ownerScope), http.MethodPost, "/v1/admin/places", body)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a non-admin create", rec.Code)
	}
	if service.calls != 0 {
		t.Fatal("the service was reached without admin.access")
	}

	rec = bearerDo(t, backoffice(service, "admin-1", adminScope), http.MethodPost, "/v1/admin/places", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", rec.Code, rec.Body.String())
	}
	if service.gotInput.OwnerUserID == nil || *service.gotInput.OwnerUserID != "0b1c2d3e-0000-4000-8000-000000000001" {
		t.Errorf("owner not assigned: %+v", service.gotInput.OwnerUserID)
	}
}

func TestPlacesBackofficeCreateValidatesTheModelShapes(t *testing.T) {
	service := &fakePlaceAdmin{}
	// Country is not two uppercase letters and the coordinate is out of range.
	rec := bearerDo(t, backoffice(service, "admin-1", adminScope), http.MethodPost, "/v1/admin/places",
		`{"name":"X","title":"X","coPlaceCategory":"0b1c2d3e-0000-4000-8000-00000000000a",
		  "country":"ve","timezone":"America/Caracas","latitude":91,"longitude":-66.9}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	_, fields := errorFields(t, rec)
	seen := map[string]bool{}
	for _, field := range fields {
		seen[field.Field] = true
	}
	if !seen["country"] || !seen["latitude"] {
		t.Fatalf("fields = %+v, want country and latitude", fields)
	}
	if service.calls != 0 {
		t.Fatal("the service was reached with invalid shapes")
	}
}

func TestPlacesBackofficeInvisibleRowIs404NotForbidden(t *testing.T) {
	// The service reports ErrNotFound because the row is outside the viewer's scope. The
	// transport must not turn that into a 403: a 403 confirms the row exists.
	service := &fakePlaceAdmin{err: placeadmin.ErrNotFound}
	for _, method := range []string{http.MethodGet, http.MethodPatch} {
		target := "/v1/admin/places/" + adminPlaceID
		body := ""
		if method == http.MethodPatch {
			body = `{"title":"Nuevo"}`
		}
		rec := bearerDo(t, backoffice(service, "editor", ownerScope), method, target, body)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", method, rec.Code)
		}
	}
}

func TestPlacesBackofficeUnknownCategoryIsAFieldError(t *testing.T) {
	service := &fakePlaceAdmin{err: placeadmin.ErrUnknownCategory}
	rec := bearerDo(t, backoffice(service, "admin-1", adminScope), http.MethodPost, "/v1/admin/places",
		`{"name":"X","title":"X","coPlaceCategory":"0b1c2d3e-0000-4000-8000-00000000000a",
		  "country":"VE","timezone":"America/Caracas","latitude":10.4,"longitude":-66.9}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	_, fields := errorFields(t, rec)
	if len(fields) == 0 || fields[0].Field != "coPlaceCategory" || fields[0].Code != "unknown" {
		t.Fatalf("fields = %+v, want coPlaceCategory/unknown", fields)
	}
}

func TestPlacesBackofficeOwnerCannotReassignOwnership(t *testing.T) {
	// Only admin.access may move the owner. An editor sending coOwnerUser must not have it
	// applied, and must not be errored out either: the field is simply not theirs to set.
	service := &fakePlaceAdmin{place: placeadmin.Place{ID: adminPlaceID}}
	rec := bearerDo(t, backoffice(service, "editor", ownerScope), http.MethodPatch,
		"/v1/admin/places/"+adminPlaceID, `{"title":"Nuevo","coOwnerUser":"0b1c2d3e-0000-4000-8000-000000000001"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if service.gotUpdate.OwnerUserID != nil {
		t.Errorf("an owner-editor reassigned ownership: %+v", *service.gotUpdate.OwnerUserID)
	}
	if service.gotUpdate.Title == nil || *service.gotUpdate.Title != "Nuevo" {
		t.Errorf("title not applied: %+v", service.gotUpdate.Title)
	}
}

func TestPlacesBackofficeRetireIs204(t *testing.T) {
	service := &fakePlaceAdmin{}
	rec := bearerDo(t, backoffice(service, "admin-1", adminScope), http.MethodDelete, "/v1/admin/places/"+adminPlaceID, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

func TestPlacesBackofficeRequiresAuthentication(t *testing.T) {
	rec := do(t, backoffice(&fakePlaceAdmin{}, "user-1", adminScope), http.MethodGet, "/v1/admin/places")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// scopeFor picks the permission set of a case.
func scopeFor(admin bool) grantedPermissions {
	if admin {
		return adminScope
	}
	return ownerScope
}

var _ = strings.TrimSpace

func TestPlaceCategoriesArePublicAndFilterable(t *testing.T) {
	service := &fakePlaceAdmin{categories: []placeadmin.Category{
		{ID: "c1", Code: "arepera", Name: "Arepera", Status: "ACTIVE"},
	}}
	// No bearer token: the taxonomy is public, so the route must answer without one.
	rec := do(t, backoffice(service, "", adminScope), http.MethodGet, "/v1/place-categories?status=ACTIVE")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if service.gotStatus != "ACTIVE" {
		t.Fatalf("status filter = %q, want ACTIVE", service.gotStatus)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"code":"arepera"`) || !strings.Contains(body, `"total":1`) {
		t.Fatalf("body = %s", body)
	}
}

func TestPlaceCategoriesRejectAnUnknownStatus(t *testing.T) {
	service := &fakePlaceAdmin{}
	rec := do(t, backoffice(service, "", adminScope), http.MethodGet, "/v1/place-categories?status=GONE")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if service.calls != 0 {
		t.Fatal("the service was reached with an invalid status")
	}
}

// The content of a place: weekly hours and attributes. These are what make the already-shipped
// `openAt` and attribute filters operable instead of SQL-only.

func (f *fakePlaceAdmin) Schedules(_ context.Context, _ placeadmin.Viewer, _ string) ([]placeadmin.Schedule, error) {
	f.calls++
	return f.schedules, f.err
}

func (f *fakePlaceAdmin) ReplaceSchedules(_ context.Context, _ placeadmin.Viewer, _, _ string, schedules []placeadmin.Schedule) error {
	f.calls++
	f.gotSchedules = schedules
	return f.err
}

func (f *fakePlaceAdmin) Attributes(_ context.Context, _ placeadmin.Viewer, _ string) ([]string, error) {
	f.calls++
	return f.attributes, f.err
}

func (f *fakePlaceAdmin) ReplaceAttributes(_ context.Context, _ placeadmin.Viewer, _, _ string, codes []string) error {
	f.calls++
	f.gotCodes = codes
	return f.err
}

func TestReplaceSchedulesSendsTheWholeSet(t *testing.T) {
	service := &fakePlaceAdmin{}
	body := `{"schedules":[{"dayOfWeek":6,"open":"20:00","close":"02:00"}]}`
	rec := bearerDo(t, backoffice(service, "owner-1", adminScope), http.MethodPut,
		"/v1/admin/places/"+adminPlaceID+"/schedules", body)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if len(service.gotSchedules) != 1 || service.gotSchedules[0].Close != "02:00" {
		t.Fatalf("schedules = %+v", service.gotSchedules)
	}
}

func TestSchedulesReadReturnsTheSet(t *testing.T) {
	service := &fakePlaceAdmin{schedules: []placeadmin.Schedule{{DayOfWeek: 1, Open: "10:00", Close: "22:00"}}}
	rec := bearerDo(t, backoffice(service, "owner-1", adminScope), http.MethodGet,
		"/v1/admin/places/"+adminPlaceID+"/schedules", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"close":"22:00"`) {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestUnknownAttributeIsAFieldError(t *testing.T) {
	// Silently dropping the code would leave the operator believing the filter will match.
	service := &fakePlaceAdmin{err: &placeadmin.UnknownAttributeError{Codes: []string{"inventado"}}}
	rec := bearerDo(t, backoffice(service, "owner-1", adminScope), http.MethodPut,
		"/v1/admin/places/"+adminPlaceID+"/attributes", `{"codes":["inventado"]}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (%s)", rec.Code, rec.Body.String())
	}
	_, fields := errorFields(t, rec)
	if len(fields) == 0 || fields[0].Field != "codes" || fields[0].Code != "unknown" {
		t.Fatalf("fields = %+v", fields)
	}
}

func TestScheduleValidationFailureIs422(t *testing.T) {
	service := &fakePlaceAdmin{err: errors.New("placeadmin: open and close are the same time")}
	rec := bearerDo(t, backoffice(service, "owner-1", adminScope), http.MethodPut,
		"/v1/admin/places/"+adminPlaceID+"/schedules", `{"schedules":[{"dayOfWeek":1,"open":"10:00","close":"10:00"}]}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (%s)", rec.Code, rec.Body.String())
	}
}
