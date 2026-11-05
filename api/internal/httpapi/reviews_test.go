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

	"github.com/ivvanare/ecosystem_complete/api/internal/reviews"
	"github.com/ivvanare/ecosystem_complete/api/internal/session"
)

// fakeReviews is a scriptable ReviewService. With panicOnCall it proves validation and
// auth run before the service: an invalid request never reaches it (S3 spirit).
type fakeReviews struct {
	listResult  reviews.ListResult
	listErr     error
	created     reviews.Review
	createErr   error
	updated     reviews.Review
	updateErr   error
	deleteErr   error
	moderated   reviews.Review
	moderateErr error
	panicOnCall bool
	// Admin listing surface.
	adminResult reviews.ListResult
	adminFilter reviews.AdminFilter
	adminPage   int
	adminCalled bool

	calls       int
	gotViewer   reviews.Viewer
	gotActor    string
	gotPlace    string
	gotReviewID string
	gotStatus   string
	gotCreate   reviews.CreateInput
	gotUpdate   reviews.UpdateInput
}

func (f *fakeReviews) guard() {
	f.calls++
	if f.panicOnCall {
		panic("the service must not be called for an invalid request")
	}
}

func (f *fakeReviews) List(_ context.Context, placeID string, viewer reviews.Viewer, page, limit int) (reviews.ListResult, error) {
	f.guard()
	f.gotPlace, f.gotViewer = placeID, viewer
	f.listResult.Page, f.listResult.Limit = page, limit
	return f.listResult, f.listErr
}

func (f *fakeReviews) Create(_ context.Context, actorID, placeID string, in reviews.CreateInput) (reviews.Review, error) {
	f.guard()
	f.gotActor, f.gotPlace, f.gotCreate = actorID, placeID, in
	return f.created, f.createErr
}

func (f *fakeReviews) Update(_ context.Context, actorID, placeID, reviewID string, in reviews.UpdateInput) (reviews.Review, error) {
	f.guard()
	f.gotActor, f.gotPlace, f.gotReviewID, f.gotUpdate = actorID, placeID, reviewID, in
	return f.updated, f.updateErr
}

func (f *fakeReviews) Delete(_ context.Context, actorID, placeID, reviewID string) error {
	f.guard()
	f.gotActor, f.gotPlace, f.gotReviewID = actorID, placeID, reviewID
	return f.deleteErr
}

func (f *fakeReviews) Moderate(_ context.Context, actorID, reviewID, status string) (reviews.Review, error) {
	f.guard()
	f.gotActor, f.gotReviewID, f.gotStatus = actorID, reviewID, status
	return f.moderated, f.moderateErr
}

// ListAll is the moderator listing.
func (f *fakeReviews) ListAll(_ context.Context, filter reviews.AdminFilter, page, _ int) (reviews.ListResult, error) {
	f.guard()
	f.adminCalled = true
	f.adminFilter = filter
	f.adminPage = page
	return f.adminResult, nil
}

const (
	reviewPlace    = "9f1c0e0a-0000-4000-8000-000000000001"
	reviewID       = "7a2b0e0a-0000-4000-8000-000000000002"
	reviewAuthorID = "3c4d0e0a-0000-4000-8000-000000000003"
)

// reviewBody is a complete Review as the contract documents it (openapi.yaml Review).
func reviewBody() reviews.Review {
	name := "Ana"
	comment := "la mejor"
	return reviews.Review{
		ID: reviewID, PlaceID: reviewPlace, AuthorID: "user-1", AuthorName: &name,
		Rating: 5, Comment: &comment, Status: reviews.StatusActive,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
}

func newReviewsHandler(service ReviewService, parser TokenParser, authorizer Authorizer) http.Handler {
	return New(Options{
		Reviews:     service,
		TokenParser: parser,
		Authorizer:  authorizer,
		Logger:      slog.New(slog.DiscardHandler),
	})
}

// authedParser accepts any token as the given user.
func authedParser(userID string) *fakeParser {
	parser := &fakeParser{}
	parser.claims.Subject = userID
	return parser
}

// authed sends a request with a bearer token the fake parser accepts as wantUser.
func authed(t *testing.T, service ReviewService, wantUser string, authorizer Authorizer, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	parser := &fakeParser{}
	parser.claims.Subject = wantUser
	if authorizer == nil {
		authorizer = &fakeAuthorizer{}
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer x")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	newReviewsHandler(service, parser, authorizer).ServeHTTP(rec, req)
	return rec
}

// The edge rules of S9 and the repo rules: uuid path values, rating bounds, the
// comment bound and the status enum are all 422 validation_failed with the closed
// field vocabulary, and none of them reaches the service.
func TestReviewsValidationMatrix(t *testing.T) {
	overlong := strings.Repeat("á", 2001)

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		field  string
		code   string
	}{
		{"list place not a uuid", http.MethodGet, "/v1/places/not-a-uuid/reviews", "", "coPlace", "invalid"},
		{"create place not a uuid", http.MethodPost, "/v1/places/not-a-uuid/reviews", `{"rating":5}`, "coPlace", "invalid"},
		{"create rating missing", http.MethodPost, "/v1/places/" + reviewPlace + "/reviews", `{"comment":"ok"}`, "rating", "required"},
		{"create rating zero", http.MethodPost, "/v1/places/" + reviewPlace + "/reviews", `{"rating":0}`, "rating", "invalid"},
		{"create rating six", http.MethodPost, "/v1/places/" + reviewPlace + "/reviews", `{"rating":6}`, "rating", "invalid"},
		{"create comment over the bound", http.MethodPost, "/v1/places/" + reviewPlace + "/reviews", `{"rating":5,"comment":"` + overlong + `"}`, "comment", "too_long"},
		{"update place not a uuid", http.MethodPatch, "/v1/places/not-a-uuid/reviews/" + reviewID, `{"rating":4}`, "coPlace", "invalid"},
		{"update review not a uuid", http.MethodPatch, "/v1/places/" + reviewPlace + "/reviews/not-a-uuid", `{"rating":4}`, "coReview", "invalid"},
		{"update no fields", http.MethodPatch, "/v1/places/" + reviewPlace + "/reviews/" + reviewID, `{}`, "rating", "required"},
		{"update rating out of range", http.MethodPatch, "/v1/places/" + reviewPlace + "/reviews/" + reviewID, `{"rating":7}`, "rating", "invalid"},
		{"update comment over the bound", http.MethodPatch, "/v1/places/" + reviewPlace + "/reviews/" + reviewID, `{"comment":"` + overlong + `"}`, "comment", "too_long"},
		{"delete review not a uuid", http.MethodDelete, "/v1/places/" + reviewPlace + "/reviews/not-a-uuid", "", "coReview", "invalid"},
		{"moderate review not a uuid", http.MethodPatch, "/v1/admin/reviews/not-a-uuid", `{"status":"MODERATED"}`, "coReview", "invalid"},
		{"moderate status missing", http.MethodPatch, "/v1/admin/reviews/" + reviewID, `{}`, "status", "required"},
		{"moderate status outside the enum", http.MethodPatch, "/v1/admin/reviews/" + reviewID, `{"status":"BANNED"}`, "status", "invalid"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := authed(t, &fakeReviews{panicOnCall: true}, "user-1", &fakeAuthorizer{allowed: true}, tc.method, tc.path, tc.body)

			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422 (%s)", rec.Code, rec.Body.String())
			}
			code, fields := errorFields(t, rec)
			if code != "validation_failed" {
				t.Errorf("error code = %q, want validation_failed", code)
			}
			if len(fields) == 0 {
				t.Fatalf("no field errors in %s", rec.Body.String())
			}
			if fields[0].Field != tc.field || fields[0].Code != tc.code {
				t.Errorf("fields[0] = %+v, want {%s %s}", fields[0], tc.field, tc.code)
			}
		})
	}
}

// The comment bound counts characters, not bytes: 2000 accented characters pass (and
// travel within the request budget), 2001 fail.
func TestReviewCommentCountsRunes(t *testing.T) {
	service := &fakeReviews{created: reviewBody()}
	rec := authed(t, service, "user-1", nil, http.MethodPost, "/v1/places/"+reviewPlace+"/reviews",
		`{"rating":5,"comment":"`+strings.Repeat("á", 2000)+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 for exactly 2000 characters (%s)", rec.Code, rec.Body.String())
	}
}

// A malformed body is 400 invalid_json, like every other endpoint.
func TestCreateReviewMalformedJSON(t *testing.T) {
	rec := authed(t, &fakeReviews{panicOnCall: true}, "user-1", nil, http.MethodPost, "/v1/places/"+reviewPlace+"/reviews", `{"rating":`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
	code, _ := errorFields(t, rec)
	if code != "invalid_json" {
		t.Errorf("error code = %q, want invalid_json", code)
	}
}

func TestCreateReviewMapsCreated(t *testing.T) {
	service := &fakeReviews{created: reviewBody()}
	rec := authed(t, service, "user-1", nil, http.MethodPost, "/v1/places/"+reviewPlace+"/reviews",
		`{"rating":5,"comment":"la mejor"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", rec.Code, rec.Body.String())
	}
	if service.gotActor != "user-1" || service.gotPlace != reviewPlace {
		t.Errorf("actor/place = %q/%q", service.gotActor, service.gotPlace)
	}
	if service.gotCreate.Rating != 5 || service.gotCreate.Comment == nil || *service.gotCreate.Comment != "la mejor" {
		t.Errorf("input = %+v", service.gotCreate)
	}

	var body struct {
		CoReview string  `json:"coReview"`
		CoPlace  string  `json:"coPlace"`
		Rating   int     `json:"rating"`
		Comment  *string `json:"comment"`
		Status   string  `json:"status"`
		Author   struct {
			ID   string  `json:"id"`
			Name *string `json:"name"`
		} `json:"author"`
		CreatedAt time.Time `json:"createdAt"`
		UpdatedAt time.Time `json:"updatedAt"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, rec.Body.String())
	}
	if body.CoReview != reviewID || body.CoPlace != reviewPlace || body.Rating != 5 || body.Status != "ACTIVE" {
		t.Errorf("body = %+v", body)
	}
	if body.Author.ID != "user-1" || body.Author.Name == nil || *body.Author.Name != "Ana" {
		t.Errorf("author = %+v", body.Author)
	}
	// Display-safe author fields only: the email never leaks.
	if strings.Contains(rec.Body.String(), "email") {
		t.Errorf("response leaks the author email: %s", rec.Body.String())
	}
}

// §8 item 15 / S10: the partial unique index surfaces as 409 review_already_exists —
// the only new error code of this commit.
func TestCreateReviewConflict(t *testing.T) {
	service := &fakeReviews{createErr: reviews.ErrAlreadyExists}
	rec := authed(t, service, "user-1", nil, http.MethodPost, "/v1/places/"+reviewPlace+"/reviews", `{"rating":5}`)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
	code, _ := errorFields(t, rec)
	if code != "review_already_exists" {
		t.Errorf("error code = %q, want review_already_exists", code)
	}
}

func TestCreateReviewRequiresAuth(t *testing.T) {
	service := &fakeReviews{panicOnCall: true}
	req := httptest.NewRequest(http.MethodPost, "/v1/places/"+reviewPlace+"/reviews", strings.NewReader(`{"rating":5}`))
	rec := httptest.NewRecorder()
	newReviewsHandler(service, &fakeParser{err: session.ErrInvalidToken}, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (%s)", rec.Code, rec.Body.String())
	}
	code, _ := errorFields(t, rec)
	if code != "unauthorized" {
		t.Errorf("error code = %q, want unauthorized", code)
	}
}

// S14: a soft-deleted or INACTIVE place is 404 on the write path too.
func TestCreateReviewOnADeadPlace(t *testing.T) {
	service := &fakeReviews{createErr: reviews.ErrNotFound}
	rec := authed(t, service, "user-1", nil, http.MethodPost, "/v1/places/"+reviewPlace+"/reviews", `{"rating":5}`)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
	code, _ := errorFields(t, rec)
	if code != "not_found" {
		t.Errorf("error code = %q, want not_found", code)
	}
}

// §8 item 17 / S11: PATCH by anyone but the author is 403 permission_denied.
func TestUpdateReviewByNonAuthor(t *testing.T) {
	service := &fakeReviews{updateErr: reviews.ErrForbidden}
	rec := authed(t, service, "user-2", nil, http.MethodPatch, "/v1/places/"+reviewPlace+"/reviews/"+reviewID, `{"rating":4}`)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (%s)", rec.Code, rec.Body.String())
	}
	code, _ := errorFields(t, rec)
	if code != "permission_denied" {
		t.Errorf("error code = %q, want permission_denied", code)
	}
}

func TestUpdateReviewOK(t *testing.T) {
	service := &fakeReviews{updated: reviewBody()}
	rec := authed(t, service, "user-1", nil, http.MethodPatch, "/v1/places/"+reviewPlace+"/reviews/"+reviewID, `{"rating":4,"comment":"bien"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if service.gotActor != "user-1" || service.gotPlace != reviewPlace || service.gotReviewID != reviewID {
		t.Errorf("actor/place/review = %q/%q/%q", service.gotActor, service.gotPlace, service.gotReviewID)
	}
	if service.gotUpdate.Rating == nil || *service.gotUpdate.Rating != 4 {
		t.Errorf("rating = %+v, want 4", service.gotUpdate.Rating)
	}
	if service.gotUpdate.Comment == nil || *service.gotUpdate.Comment != "bien" {
		t.Errorf("comment = %+v", service.gotUpdate.Comment)
	}
}

func TestUpdateReviewMissingIsNotFound(t *testing.T) {
	service := &fakeReviews{updateErr: reviews.ErrNotFound}
	rec := authed(t, service, "user-1", nil, http.MethodPatch, "/v1/places/"+reviewPlace+"/reviews/"+reviewID, `{"rating":4}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
}

// S11: DELETE is author-only; a non-author gets 403.
func TestDeleteReviewByNonAuthor(t *testing.T) {
	service := &fakeReviews{deleteErr: reviews.ErrForbidden}
	rec := authed(t, service, "user-2", nil, http.MethodDelete, "/v1/places/"+reviewPlace+"/reviews/"+reviewID, "")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (%s)", rec.Code, rec.Body.String())
	}
	code, _ := errorFields(t, rec)
	if code != "permission_denied" {
		t.Errorf("error code = %q, want permission_denied", code)
	}
}

func TestDeleteReviewOK(t *testing.T) {
	service := &fakeReviews{}
	rec := authed(t, service, "user-1", nil, http.MethodDelete, "/v1/places/"+reviewPlace+"/reviews/"+reviewID, "")

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (%s)", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Errorf("a delete answers no body: %s", rec.Body.String())
	}
	if service.gotActor != "user-1" || service.gotReviewID != reviewID {
		t.Errorf("actor/review = %q/%q", service.gotActor, service.gotReviewID)
	}
}

// §8 item 18: an anonymous caller is a zero viewer — the visibility rule in the store
// hides non-ACTIVE rows from them.
func TestListReviewsAnonymousViewer(t *testing.T) {
	service := &fakeReviews{listResult: reviews.ListResult{Rows: []reviews.Review{reviewBody()}, Total: 1}}
	req := httptest.NewRequest(http.MethodGet, "/v1/places/"+reviewPlace+"/reviews?page=1&limit=20", nil)
	rec := httptest.NewRecorder()
	newReviewsHandler(service, &fakeParser{}, &fakeAuthorizer{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if service.gotViewer != (reviews.Viewer{}) {
		t.Errorf("viewer = %+v, want anonymous", service.gotViewer)
	}
	if service.gotPlace != reviewPlace {
		t.Errorf("place = %q", service.gotPlace)
	}

	var body struct {
		Data []struct {
			CoReview string `json:"coReview"`
			Author   struct {
				ID   string  `json:"id"`
				Name *string `json:"name"`
			} `json:"author"`
		} `json:"data"`
		Page  int `json:"page"`
		Limit int `json:"limit"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, rec.Body.String())
	}
	if body.Total != 1 || len(body.Data) != 1 || body.Page != 1 || body.Limit != 20 {
		t.Errorf("body = %+v", body)
	}
}

// §8 item 18: with a bearer the list knows the author, and a live review.moderate
// check widens visibility. A failed check narrows, never errors the read.
func TestListReviewsResolvesTheViewer(t *testing.T) {
	service := &fakeReviews{}
	rec := authed(t, service, "user-1", &fakeAuthorizer{allowed: true}, http.MethodGet, "/v1/places/"+reviewPlace+"/reviews", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if service.gotViewer != (reviews.Viewer{UserID: "user-1", Moderator: true}) {
		t.Errorf("viewer = %+v, want the author with moderator visibility", service.gotViewer)
	}

	degraded := &fakeReviews{}
	rec = authed(t, degraded, "user-1", &fakeAuthorizer{err: errors.New("db down")}, http.MethodGet, "/v1/places/"+reviewPlace+"/reviews", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: a failed permission check degrades, never errors", rec.Code)
	}
	if degraded.gotViewer != (reviews.Viewer{UserID: "user-1"}) {
		t.Errorf("viewer = %+v, want the author without the moderator flag", degraded.gotViewer)
	}
}

func TestListReviewsOnADeadPlace(t *testing.T) {
	service := &fakeReviews{listErr: reviews.ErrNotFound}
	req := httptest.NewRequest(http.MethodGet, "/v1/places/"+reviewPlace+"/reviews", nil)
	rec := httptest.NewRecorder()
	newReviewsHandler(service, &fakeParser{}, &fakeAuthorizer{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
	code, _ := errorFields(t, rec)
	if code != "not_found" {
		t.Errorf("error code = %q, want not_found", code)
	}
}

// §8 item 19 / S12: moderation requires the seeded review.moderate capability.
func TestModerateReviewRequiresCapability(t *testing.T) {
	service := &fakeReviews{panicOnCall: true}
	rec := authed(t, service, "user-1", &fakeAuthorizer{allowed: false}, http.MethodPatch, "/v1/admin/reviews/"+reviewID, `{"status":"MODERATED"}`)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (%s)", rec.Code, rec.Body.String())
	}
	code, _ := errorFields(t, rec)
	if code != "permission_denied" {
		t.Errorf("error code = %q, want permission_denied", code)
	}
}

func TestModerateReviewOK(t *testing.T) {
	moderated := reviewBody()
	moderated.Status = reviews.StatusModerated
	service := &fakeReviews{moderated: moderated}
	rec := authed(t, service, "admin-1", &fakeAuthorizer{allowed: true}, http.MethodPatch, "/v1/admin/reviews/"+reviewID, `{"status":"MODERATED"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if service.gotActor != "admin-1" || service.gotReviewID != reviewID || service.gotStatus != reviews.StatusModerated {
		t.Errorf("actor/review/status = %q/%q/%q", service.gotActor, service.gotReviewID, service.gotStatus)
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, rec.Body.String())
	}
	if body.Status != reviews.StatusModerated {
		t.Errorf("status = %q, want MODERATED", body.Status)
	}
}

func TestModerateReviewMissingIsNotFound(t *testing.T) {
	service := &fakeReviews{moderateErr: reviews.ErrNotFound}
	rec := authed(t, service, "admin-1", &fakeAuthorizer{allowed: true}, http.MethodPatch, "/v1/admin/reviews/"+reviewID, `{"status":"INACTIVE"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
}

// T10: the routes exist only when the service is wired, like the broker-gated ones.
func TestReviewRoutesAreAbsentWhenNotWired(t *testing.T) {
	handler := New(Options{Logger: slog.New(slog.DiscardHandler)})
	for _, target := range []struct{ method, path string }{
		{http.MethodGet, "/v1/places/" + reviewPlace + "/reviews"},
		{http.MethodPost, "/v1/places/" + reviewPlace + "/reviews"},
		{http.MethodPatch, "/v1/places/" + reviewPlace + "/reviews/" + reviewID},
		{http.MethodDelete, "/v1/places/" + reviewPlace + "/reviews/" + reviewID},
		{http.MethodPatch, "/v1/admin/reviews/" + reviewID},
	} {
		rec := do(t, handler, target.method, target.path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404 when unwired", target.method, target.path, rec.Code)
		}
	}
}

// The moderator listing: without it the moderation route had no entry point (L14), so
// these tests pin the filters, the vocabulary and the permission gate.

func TestAdminReviewsListPassesFiltersThrough(t *testing.T) {
	service := &fakeReviews{adminResult: reviews.ListResult{Total: 1, Page: 2, Limit: 20}}
	handler := newReviewsHandler(service, authedParser(reviewAuthorID), &fakeAuthorizer{allowed: true})

	rec := bearerDo(t, handler, http.MethodGet,
		"/v1/admin/reviews?status=MODERATED&placeId="+reviewPlace+"&authorId="+reviewAuthorID+"&page=2&limit=20", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if service.adminFilter.Status != reviews.StatusModerated {
		t.Errorf("status filter = %q, want MODERATED", service.adminFilter.Status)
	}
	if service.adminFilter.PlaceID != reviewPlace || service.adminFilter.AuthorID != reviewAuthorID {
		t.Errorf("filters = %+v", service.adminFilter)
	}
	if service.adminPage != 2 {
		t.Errorf("page = %d, want 2", service.adminPage)
	}
}

func TestAdminReviewsRejectsAnUnknownStatusBeforeTheService(t *testing.T) {
	service := &fakeReviews{}
	rec := bearerDo(t, newReviewsHandler(service, authedParser(reviewAuthorID), &fakeAuthorizer{allowed: true}), http.MethodGet, "/v1/admin/reviews?status=DELETED", "")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	code, fields := errorFields(t, rec)
	if code != "validation_failed" || len(fields) == 0 || fields[0].Field != "status" {
		t.Fatalf("error = %s %+v", code, fields)
	}
	if service.adminCalled {
		t.Fatal("the service was called with an invalid status")
	}
}

func TestAdminReviewsRejectsMalformedIDs(t *testing.T) {
	rec := bearerDo(t, newReviewsHandler(&fakeReviews{}, authedParser(reviewAuthorID), &fakeAuthorizer{allowed: true}), http.MethodGet,
		"/v1/admin/reviews?placeId=not-a-uuid", "")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	_, fields := errorFields(t, rec)
	if len(fields) == 0 || fields[0].Field != "placeId" {
		t.Fatalf("fields = %+v, want placeId", fields)
	}
}

func TestAdminReviewsRequiresTheModerationCapability(t *testing.T) {
	// The route is gated by review.moderate: an authenticated caller without it is
	// forbidden, and the service is never reached.
	handler := New(Options{
		Reviews:     &fakeReviews{},
		TokenParser: authedParser(reviewAuthorID),
		Authorizer:  &fakeAuthorizer{allowed: false},
		Logger:      slog.New(slog.DiscardHandler),
	})
	rec := bearerDo(t, handler, http.MethodGet, "/v1/admin/reviews", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}
