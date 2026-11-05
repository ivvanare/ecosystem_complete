package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/checkin"
	"github.com/ivvanare/ecosystem_complete/api/internal/follow"
	"github.com/ivvanare/ecosystem_complete/api/internal/geo"
	"github.com/ivvanare/ecosystem_complete/api/internal/session"
)

// fakeGraph is a scriptable SocialGraph.
type fakeGraph struct {
	followErr   error
	unfollowErr error
	page        follow.Page
	listErr     error
	followed    [][2]string
	removed     [][2]string
}

func (f *fakeGraph) Follow(_ context.Context, follower, followed string) error {
	if f.followErr != nil {
		return f.followErr
	}
	f.followed = append(f.followed, [2]string{follower, followed})
	return nil
}

func (f *fakeGraph) Unfollow(_ context.Context, follower, followed string) error {
	if f.unfollowErr != nil {
		return f.unfollowErr
	}
	f.removed = append(f.removed, [2]string{follower, followed})
	return nil
}

func (f *fakeGraph) List(_ context.Context, _, _ string, _, _ int) (follow.Page, error) {
	return f.page, f.listErr
}

// fakeCheckins is a scriptable Checkins.
type fakeCheckins struct {
	result  checkin.Checkin
	err     error
	where   geo.Coordinate
	placeID string
}

func (f *fakeCheckins) Create(_ context.Context, _, placeID string, where geo.Coordinate, now time.Time) (checkin.Checkin, error) {
	f.where = where
	f.placeID = placeID
	if f.err != nil {
		return checkin.Checkin{}, f.err
	}
	return checkin.Checkin{ID: "c1", PlaceID: placeID, Verification: checkin.VerificationProximity, CreatedAt: now}, nil
}

const userUUID = "588d734f-4864-4a35-8357-d7c780c7b1ad"
const targetUUID = "a0000001-0000-4000-8000-000000000001"
const placeUUID = "a0000001-0000-4000-8000-000000000001"

func graphHandler(graph SocialGraph, checkins Checkins) http.Handler {
	return New(Options{
		Follows:     graph,
		Checkins:    checkins,
		TokenParser: &fakeParser{claims: session.Claims{}},
		Logger:      slog.New(slog.DiscardHandler),
	})
}

// bearerDo sends a request with a bearer token and an optional JSON body.
func bearerDo(t *testing.T, handler http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var request *http.Request
	if body == "" {
		request = httptest.NewRequest(method, target, nil)
	} else {
		request = httptest.NewRequest(method, target, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, request)
	return rec
}

func TestFollowRequiresAuthentication(t *testing.T) {
	rec := do(t, graphHandler(&fakeGraph{}, &fakeCheckins{}), http.MethodPost, "/v1/follows")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestFollowRejectsSelfFollowAsValidation(t *testing.T) {
	graph := &fakeGraph{followErr: follow.ErrSelfFollow}
	rec := bearerDo(t, graphHandler(graph, nil), http.MethodPost, "/v1/follows", `{"coUser":"`+targetUUID+`"}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (%s)", rec.Code, rec.Body.String())
	}
	code, fields := errorFields(t, rec)
	if code != "validation_failed" || len(fields) == 0 || fields[0].Field != "coUser" {
		t.Fatalf("error = %s %+v, want validation_failed on coUser", code, fields)
	}
}

func TestFollowAnswers404ForAnUnknownTarget(t *testing.T) {
	graph := &fakeGraph{followErr: follow.ErrUnknownUser}
	rec := bearerDo(t, graphHandler(graph, nil), http.MethodPost, "/v1/follows", `{"coUser":"`+targetUUID+`"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestFollowAndUnfollowAnswer204(t *testing.T) {
	graph := &fakeGraph{}
	handler := graphHandler(graph, nil)
	if rec := bearerDo(t, handler, http.MethodPost, "/v1/follows", `{"coUser":"`+targetUUID+`"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("follow status = %d, want 204", rec.Code)
	}
	if rec := bearerDo(t, handler, http.MethodDelete, "/v1/follows/"+targetUUID, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("unfollow status = %d, want 204", rec.Code)
	}
}

func TestCheckinReportsNotNearPlace(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
		code string
	}{
		{"outside the radius", checkin.ErrNotNearPlace, http.StatusUnprocessableEntity, "not_near_place"},
		{"already today", checkin.ErrAlreadyCheckedIn, http.StatusConflict, "already_checked_in"},
		{"place gone", checkin.ErrPlaceNotFound, http.StatusNotFound, "not_found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := graphHandler(&fakeGraph{}, &fakeCheckins{err: tc.err})
			rec := bearerDo(t, handler, http.MethodPost, "/v1/places/"+placeUUID+"/checkins", `{"near":"10.4960,-66.8530"}`)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.want, rec.Body.String())
			}
			code, _ := errorFields(t, rec)
			if code != tc.code {
				t.Fatalf("error code = %q, want %q", code, tc.code)
			}
		})
	}
}

func TestCheckinRejectsAMalformedCoordinateBeforeTheService(t *testing.T) {
	checkins := &fakeCheckins{}
	handler := graphHandler(&fakeGraph{}, checkins)
	rec := bearerDo(t, handler, http.MethodPost, "/v1/places/"+placeUUID+"/checkins", `{"near":"abc"}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if checkins.placeID != "" {
		t.Fatal("the service was called with an invalid coordinate")
	}
}

func TestCheckinReturnsTheVerdictNeverTheCoordinate(t *testing.T) {
	handler := graphHandler(&fakeGraph{}, &fakeCheckins{})
	rec := bearerDo(t, handler, http.MethodPost, "/v1/places/"+placeUUID+"/checkins", `{"near":"10.4969,-66.8530"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"PROXIMITY"`) {
		t.Fatalf("body = %s, want the verification verdict", body)
	}
	// The coordinate that proved presence must not travel back: D11 has no coordinates.
	if strings.Contains(body, "10.4969") || strings.Contains(body, "-66.8530") {
		t.Fatalf("response echoes the coordinate: %s", body)
	}
}

func TestGraphRoutesAreAbsentWhenNotWired(t *testing.T) {
	rec := do(t, New(Options{Logger: slog.New(slog.DiscardHandler)}), http.MethodGet, "/v1/follows")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when Follows is not wired", rec.Code)
	}
}

func TestListFollowsRejectsAnUnknownDirection(t *testing.T) {
	rec := bearerDo(t, graphHandler(&fakeGraph{}, nil), http.MethodGet, "/v1/follows?direction=enemies", "")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	code, fields := errorFields(t, rec)
	if code != "validation_failed" || fields[0].Field != "direction" {
		t.Fatalf("error = %s %+v", code, fields)
	}
}

func TestListFollowsMapsMutual(t *testing.T) {
	graph := &fakeGraph{page: follow.Page{
		Members: []follow.Member{{UserID: targetUUID, Name: "Amiga", Mutual: true}},
		Total:   1, Page: 1, Limit: 20,
	}}
	rec := bearerDo(t, graphHandler(graph, nil), http.MethodGet, "/v1/follows", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"mutual":true`) {
		t.Fatalf("body = %s, want mutual true", rec.Body.String())
	}
}

func TestFollowPropagatesUnknownFailure(t *testing.T) {
	graph := &fakeGraph{followErr: errors.New("db down")}
	rec := bearerDo(t, graphHandler(graph, nil), http.MethodPost, "/v1/follows", `{"coUser":"`+targetUUID+`"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
