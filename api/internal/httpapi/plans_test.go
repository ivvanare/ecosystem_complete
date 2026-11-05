package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/ivvanare/ecosystem_complete/api/internal/invitations"
	"github.com/ivvanare/ecosystem_complete/api/internal/plans"
)

// fakePlans scripts the collaboration service and records what it was asked.
type fakePlans struct {
	plan        plans.Plan
	err         error
	gotIn       plans.CreateInput
	gotUpd      plans.UpdateInput
	gotJoin     string
	gotLeft     [2]string
	places      []plans.PlanPlace
	gotPlace    string
	gotOrder    int
	gotVote     int
	gotInvite   invitations.CreateInput
	invitations []invitations.Invitation
	gotAnswer   string
	calls       int
}

func (f *fakePlans) Create(_ context.Context, _ string, in plans.CreateInput) (plans.Plan, error) {
	f.calls++
	f.gotIn = in
	return f.plan, f.err
}

func (f *fakePlans) Get(context.Context, string, string) (plans.Plan, error) {
	f.calls++
	return f.plan, f.err
}

func (f *fakePlans) Update(_ context.Context, _, _ string, in plans.UpdateInput) (plans.Plan, error) {
	f.calls++
	f.gotUpd = in
	return f.plan, f.err
}

func (f *fakePlans) Join(_ context.Context, actorID, planID string) error {
	f.calls++
	f.gotJoin = actorID
	return f.err
}

func (f *fakePlans) Leave(_ context.Context, actorID, planID, targetID string) error {
	f.calls++
	f.gotLeft = [2]string{actorID, targetID}
	return f.err
}

func (f *fakePlans) Places(context.Context, string, string) ([]plans.PlanPlace, error) {
	f.calls++
	return f.places, f.err
}

func (f *fakePlans) AddPlace(_ context.Context, _, _, placeID string, order int) error {
	f.calls++
	f.gotPlace, f.gotOrder = placeID, order
	return f.err
}

func (f *fakePlans) RemovePlace(context.Context, string, string, string) error {
	f.calls++
	return f.err
}

func (f *fakePlans) Vote(_ context.Context, _, _, placeID string, value int) error {
	f.calls++
	f.gotPlace, f.gotVote = placeID, value
	return f.err
}

func (f *fakePlans) Invite(_ context.Context, _, _ string, in invitations.CreateInput) (invitations.Invitation, error) {
	f.calls++
	f.gotInvite = in
	return invitations.Invitation{ID: "i1", Status: invitations.StatusPending, Mode: in.Mode}, f.err
}
func (f *fakePlans) PlanInvitations(context.Context, string, string) ([]invitations.Invitation, error) {
	f.calls++
	return f.invitations, f.err
}
func (f *fakePlans) MyInvitations(context.Context, string) ([]invitations.Invitation, error) {
	f.calls++
	return f.invitations, f.err
}
func (f *fakePlans) VenueHidden(context.Context, string, string) (bool, error) { return false, nil }
func (f *fakePlans) AcceptInvitation(_ context.Context, _, id string) error {
	f.calls++
	f.gotAnswer = "accept:" + id
	return f.err
}
func (f *fakePlans) DeclineInvitation(_ context.Context, _, id string) error {
	f.calls++
	f.gotAnswer = "decline:" + id
	return f.err
}

func (f *fakePlans) List(context.Context, string, int, int) ([]plans.Plan, int, error) {
	f.calls++
	return []plans.Plan{f.plan}, 1, f.err
}

const planUUID = "0b1c2d3e-0000-4000-8000-000000000009"
const planOwnerUUID = "0b1c2d3e-0000-4000-8000-000000000011"
const planGuestUUID = "0b1c2d3e-0000-4000-8000-000000000012"

func plansHandler(service PlanCollaboration, userID string) http.Handler {
	return New(Options{
		Plans:       service,
		TokenParser: authedParser(userID),
		Logger:      slog.New(slog.DiscardHandler),
	})
}

func TestCreatePlanDefaultsToPrivateAndReturnsViewerRole(t *testing.T) {
	service := &fakePlans{plan: plans.Plan{ID: planUUID, Visibility: plans.VisibilityPrivate, ViewerRole: plans.RoleOwner}}
	rec := bearerDo(t, plansHandler(service, "owner-1"), http.MethodPost, "/v1/plans", `{"name":"Cena de pasta"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	// The transport must not invent a visibility: the service owns the private default.
	if service.gotIn.Visibility != "" && service.gotIn.Visibility != plans.VisibilityPrivate {
		t.Fatalf("visibility sent = %q, want the private default", service.gotIn.Visibility)
	}
	if service.gotIn.Name != "Cena de pasta" {
		t.Fatalf("name = %q", service.gotIn.Name)
	}
}

func TestCreatePlanRejectsBadShapes(t *testing.T) {
	cases := []struct{ body, field string }{
		{`{"name":""}`, "name"},
		{`{"name":"X","visibility":"SECRET"}`, "visibility"},
		{`{"name":"X","partySize":51}`, "partySize"},
		{`{"name":"X","scheduledAt":"mañana"}`, "scheduledAt"},
	}
	for _, tc := range cases {
		service := &fakePlans{}
		rec := bearerDo(t, plansHandler(service, "owner-1"), http.MethodPost, "/v1/plans", tc.body)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s: status = %d, want 422", tc.body, rec.Code)
		}
		_, fields := errorFields(t, rec)
		if len(fields) == 0 || fields[0].Field != tc.field {
			t.Fatalf("%s: fields = %+v, want %s", tc.body, fields, tc.field)
		}
		if service.calls != 0 {
			t.Fatalf("%s: the service was reached with invalid shapes", tc.body)
		}
	}
}

func TestJoinIsTheSelfJoinAndNeedsNoBody(t *testing.T) {
	service := &fakePlans{}
	rec := bearerDo(t, plansHandler(service, "guest-1"), http.MethodPost, "/v1/plans/"+planUUID+"/members", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if service.gotJoin != "guest-1" {
		t.Fatalf("joined as %q, want the caller", service.gotJoin)
	}
}

func TestJoinOnAPrivatePlanIs404(t *testing.T) {
	// The service reports ErrNotFound for a private plan: it must not confirm it exists.
	service := &fakePlans{err: plans.ErrNotFound}
	rec := bearerDo(t, plansHandler(service, "stranger"), http.MethodPost, "/v1/plans/"+planUUID+"/members", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestViewerMutatingAVisiblePlanIs403(t *testing.T) {
	// Visible-but-insufficient is 403, and it must stay distinct from the 404 above: the
	// caller can see the plan and still cannot change it.
	service := &fakePlans{err: plans.ErrForbidden}
	rec := bearerDo(t, plansHandler(service, "viewer-1"), http.MethodPatch, "/v1/plans/"+planUUID, `{"name":"Nuevo"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	code, _ := errorFields(t, rec)
	if code != "permission_denied" {
		t.Fatalf("code = %q, want permission_denied", code)
	}
}

func TestOwnerCannotLeaveAlone(t *testing.T) {
	service := &fakePlans{err: plans.ErrForbidden}
	rec := bearerDo(t, plansHandler(service, "owner-1"), http.MethodDelete,
		"/v1/plans/"+planUUID+"/members/"+planOwnerUUID, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestLeavePassesTheTargetThrough(t *testing.T) {
	service := &fakePlans{}
	rec := bearerDo(t, plansHandler(service, "owner-1"), http.MethodDelete,
		"/v1/plans/"+planUUID+"/members/"+planGuestUUID, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if service.gotLeft[0] != "owner-1" || service.gotLeft[1] != planGuestUUID {
		t.Fatalf("leave = %+v", service.gotLeft)
	}
}

func TestPlansRequireAuthentication(t *testing.T) {
	rec := do(t, plansHandler(&fakePlans{}, "x"), http.MethodGet, "/v1/plans")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestPlansResponseCarriesTheViewerRole(t *testing.T) {
	service := &fakePlans{plan: plans.Plan{ID: planUUID, Name: "Festival", Visibility: plans.VisibilityPublic, ViewerRole: plans.RoleViewer, MemberCount: 12}}
	rec := bearerDo(t, plansHandler(service, "guest-1"), http.MethodGet, "/v1/plans/"+planUUID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"viewerRole":"VIEWER"`) || !strings.Contains(body, `"memberCount":12`) {
		t.Fatalf("body = %s", body)
	}
}

func TestVoteRejectsAValueOutsideTheSet(t *testing.T) {
	service := &fakePlans{}
	rec := bearerDo(t, plansHandler(service, "member"), http.MethodPost,
		"/v1/plans/"+planUUID+"/places/"+planGuestUUID+"/votes", `{"vote":0}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if service.calls != 0 {
		t.Fatal("the service was reached with an invalid vote")
	}
}

func TestVotePassesThroughAndDefaultsOrderToOne(t *testing.T) {
	service := &fakePlans{}
	rec := bearerDo(t, plansHandler(service, "member"), http.MethodPost,
		"/v1/plans/"+planUUID+"/places/"+planGuestUUID+"/votes", `{"vote":-1}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("vote status = %d (%s)", rec.Code, rec.Body.String())
	}
	if service.gotVote != -1 {
		t.Fatalf("vote = %d, want -1", service.gotVote)
	}

	rec = bearerDo(t, plansHandler(service, "editor"), http.MethodPost,
		"/v1/plans/"+planUUID+"/places", `{"coPlace":"`+planGuestUUID+`"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("add status = %d (%s)", rec.Code, rec.Body.String())
	}
	if service.gotOrder != 1 {
		t.Fatalf("default order = %d, want 1", service.gotOrder)
	}
}

func TestItineraryCarriesNetAndMyVote(t *testing.T) {
	service := &fakePlans{places: []plans.PlanPlace{
		{PlaceID: planGuestUUID, Title: "Arepera", Order: 1, NetVotes: 3, MyVote: 1},
	}}
	rec := bearerDo(t, plansHandler(service, "member"), http.MethodGet, "/v1/plans/"+planUUID+"/places", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"netVotes":3`) || !strings.Contains(body, `"myVote":1`) {
		t.Fatalf("body = %s", body)
	}
}

// Invitations over the plan surface: the host's view, and the invitee's own inbox.

func TestInvitePassesThePayloadAndAnswers201(t *testing.T) {
	service := &fakePlans{}
	rec := bearerDo(t, plansHandler(service, "host"), http.MethodPost,
		"/v1/plans/"+planUUID+"/invitations",
		`{"coUser":"`+planGuestUUID+`","expiresAt":"2030-01-01T00:00:00Z","hint":"llevá abrigo"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if service.gotInvite.UserID != planGuestUUID || service.gotInvite.Hint == nil {
		t.Fatalf("payload = %+v", service.gotInvite)
	}
}

func TestInviteRejectsMissingExpiryAndBadShape(t *testing.T) {
	cases := []struct{ body, field string }{
		{`{"coUser":"nope","expiresAt":"2030-01-01T00:00:00Z"}`, "coUser"},
		{`{"coUser":"` + planGuestUUID + `"}`, "expiresAt"},
		{`{"coUser":"` + planGuestUUID + `","expiresAt":"mañana"}`, "expiresAt"},
	}
	for _, tc := range cases {
		service := &fakePlans{}
		rec := bearerDo(t, plansHandler(service, "host"), http.MethodPost,
			"/v1/plans/"+planUUID+"/invitations", tc.body)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s: status = %d, want 422", tc.body, rec.Code)
		}
		_, fields := errorFields(t, rec)
		if len(fields) == 0 || fields[0].Field != tc.field {
			t.Fatalf("%s: fields = %+v, want %s", tc.body, fields, tc.field)
		}
		if service.calls != 0 {
			t.Fatalf("%s: the service was reached", tc.body)
		}
	}
}

func TestInvitationAnswerRoutesCarryTheVerb(t *testing.T) {
	service := &fakePlans{}
	rec := bearerDo(t, plansHandler(service, "invitee"), http.MethodPost,
		"/v1/invitations/"+planUUID+"/accept", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("accept status = %d (%s)", rec.Code, rec.Body.String())
	}
	if service.gotAnswer != "accept:"+planUUID {
		t.Fatalf("answer = %q", service.gotAnswer)
	}

	rec = bearerDo(t, plansHandler(service, "invitee"), http.MethodPost,
		"/v1/invitations/"+planUUID+"/decline", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("decline status = %d", rec.Code)
	}
	if service.gotAnswer != "decline:"+planUUID {
		t.Fatalf("answer = %q", service.gotAnswer)
	}
}

func TestAnInvisibleInvitationIs404(t *testing.T) {
	service := &fakePlans{err: invitations.ErrNotFound}
	rec := bearerDo(t, plansHandler(service, "stranger"), http.MethodPost,
		"/v1/invitations/"+planUUID+"/accept", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestANonMemberInvitingIs403(t *testing.T) {
	service := &fakePlans{err: invitations.ErrForbidden}
	rec := bearerDo(t, plansHandler(service, "viewer"), http.MethodPost,
		"/v1/plans/"+planUUID+"/invitations",
		`{"coUser":"`+planGuestUUID+`","expiresAt":"2030-01-01T00:00:00Z"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestADuplicateLiveInvitationIs409(t *testing.T) {
	// The partial unique index owns "one live invitation per person and plan"; the transport
	// answers a conflict so a client can resolve it instead of reading a 500.
	service := &fakePlans{err: invitations.ErrAlreadyPending}
	rec := bearerDo(t, plansHandler(service, "host"), http.MethodPost,
		"/v1/plans/"+planUUID+"/invitations",
		`{"coUser":"`+planGuestUUID+`","expiresAt":"2030-01-01T00:00:00Z"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
	code, _ := errorFields(t, rec)
	if code != "invitation_already_pending" {
		t.Fatalf("code = %q", code)
	}
}
