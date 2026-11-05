package reviews

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

// fakeStore is a scriptable Store. The call counters prove the orchestration rules of
// S11/S14 run before any write: a dead place or a foreign author stops the call chain.
type fakeStore struct {
	adminRows   []Review
	adminTotal  int
	adminErr    error
	adminFilter AdminFilter
	active      bool
	activeErr   error

	listRows  []Review
	listTotal int
	listErr   error

	getReview Review
	getErr    error

	created   Review
	createErr error

	updated   Review
	updateErr error

	deleteErr error

	moderated   Review
	moderateErr error

	activeCalls   int
	listCalls     int
	getCalls      int
	createCalls   int
	updateCalls   int
	deleteCalls   int
	moderateCalls int

	gotViewer   Viewer
	gotActor    string
	gotPlace    string
	gotReviewID string
	gotPage     int
	gotLimit    int
	gotCreate   CreateInput
	gotUpdate   UpdateInput
	gotStatus   string
}

func (f *fakeStore) PlaceActive(context.Context, string) (bool, error) {
	f.activeCalls++
	return f.active, f.activeErr
}

func (f *fakeStore) List(_ context.Context, placeID string, viewer Viewer, page, limit int) ([]Review, int, error) {
	f.listCalls++
	f.gotPlace, f.gotViewer, f.gotPage, f.gotLimit = placeID, viewer, page, limit
	return f.listRows, f.listTotal, f.listErr
}

func (f *fakeStore) Get(_ context.Context, placeID, reviewID string) (Review, error) {
	f.getCalls++
	f.gotPlace, f.gotReviewID = placeID, reviewID
	return f.getReview, f.getErr
}

func (f *fakeStore) Create(_ context.Context, actorID, placeID string, in CreateInput) (Review, error) {
	f.createCalls++
	f.gotActor, f.gotPlace, f.gotCreate = actorID, placeID, in
	return f.created, f.createErr
}

func (f *fakeStore) Update(_ context.Context, actorID, reviewID string, in UpdateInput) (Review, error) {
	f.updateCalls++
	f.gotActor, f.gotReviewID, f.gotUpdate = actorID, reviewID, in
	return f.updated, f.updateErr
}

func (f *fakeStore) Delete(_ context.Context, actorID, reviewID string) error {
	f.deleteCalls++
	f.gotActor, f.gotReviewID = actorID, reviewID
	return f.deleteErr
}

func (f *fakeStore) AdminList(_ context.Context, filter AdminFilter, _, _ int) ([]Review, int, error) {
	f.adminFilter = filter
	return f.adminRows, f.adminTotal, f.adminErr
}

func (f *fakeStore) Moderate(_ context.Context, actorID, reviewID, status string) (Review, error) {
	f.moderateCalls++
	f.gotActor, f.gotReviewID, f.gotStatus = actorID, reviewID, status
	return f.moderated, f.moderateErr
}

func newService(store Store) *Service {
	return NewService(store, slog.New(slog.DiscardHandler))
}

// S14: a soft-deleted or INACTIVE place is ErrNotFound on the list, and the store's
// List is never reached.
func TestListRequiresAnActivePlace(t *testing.T) {
	store := &fakeStore{active: false}
	_, err := newService(store).List(context.Background(), "p1", Viewer{}, 1, 20)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if store.listCalls != 0 {
		t.Errorf("List called %d times with a dead place, want 0", store.listCalls)
	}
}

// The viewer and the pagination travel unchanged: the visibility rule lives in the
// store predicate, and the page is the caller's.
func TestListPassesViewerAndPagingThrough(t *testing.T) {
	store := &fakeStore{active: true, listRows: []Review{{ID: "r1"}}, listTotal: 1}
	result, err := newService(store).List(context.Background(), "p1", Viewer{UserID: "u1", Moderator: true}, 2, 50)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if store.gotViewer != (Viewer{UserID: "u1", Moderator: true}) {
		t.Errorf("viewer = %+v, want the caller's viewer", store.gotViewer)
	}
	if store.gotPage != 2 || store.gotLimit != 50 {
		t.Errorf("page/limit = %d/%d, want 2/50", store.gotPage, store.gotLimit)
	}
	if result.Total != 1 || result.Page != 2 || result.Limit != 50 || len(result.Rows) != 1 {
		t.Errorf("result = %+v", result)
	}
}

// S14 applies to the write path too: creating on a dead place is ErrNotFound and the
// insert never happens.
func TestCreateRequiresAnActivePlace(t *testing.T) {
	store := &fakeStore{active: false}
	_, err := newService(store).Create(context.Background(), "u1", "p1", CreateInput{Rating: 5})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if store.createCalls != 0 {
		t.Errorf("Create called %d times with a dead place, want 0", store.createCalls)
	}
}

// The actor travels to the store: it is who set_config announces (S13).
func TestCreatePassesTheActorThrough(t *testing.T) {
	comment := "la mejor"
	store := &fakeStore{active: true, created: Review{ID: "r1"}}
	review, err := newService(store).Create(context.Background(), "u1", "p1", CreateInput{Rating: 5, Comment: &comment})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if store.gotActor != "u1" || store.gotPlace != "p1" {
		t.Errorf("actor/place = %q/%q", store.gotActor, store.gotPlace)
	}
	if store.gotCreate.Rating != 5 || store.gotCreate.Comment == nil || *store.gotCreate.Comment != comment {
		t.Errorf("input = %+v", store.gotCreate)
	}
	if review.ID != "r1" {
		t.Errorf("review = %+v", review)
	}
}

// S11: PATCH by anyone but the author is ErrForbidden, and the write never happens.
func TestUpdateRejectsANonAuthor(t *testing.T) {
	store := &fakeStore{active: true, getReview: Review{ID: "r1", AuthorID: "author-1"}}
	_, err := newService(store).Update(context.Background(), "user-2", "p1", "r1", UpdateInput{})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	if store.updateCalls != 0 {
		t.Errorf("Update called %d times by a non-author, want 0", store.updateCalls)
	}
}

// S11: DELETE by anyone but the author is ErrForbidden, and the write never happens.
func TestDeleteRejectsANonAuthor(t *testing.T) {
	store := &fakeStore{active: true, getReview: Review{ID: "r1", AuthorID: "author-1"}}
	err := newService(store).Delete(context.Background(), "user-2", "p1", "r1")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	if store.deleteCalls != 0 {
		t.Errorf("Delete called %d times by a non-author, want 0", store.deleteCalls)
	}
}

// A missing review is ErrNotFound and the write is not attempted.
func TestUpdateMissingReviewIsNotFound(t *testing.T) {
	store := &fakeStore{active: true, getErr: ErrNotFound}
	_, err := newService(store).Update(context.Background(), "u1", "p1", "r1", UpdateInput{})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if store.updateCalls != 0 {
		t.Errorf("Update called %d times for a missing review, want 0", store.updateCalls)
	}
}

// S14 wins over the author check: on a dead place even the author gets ErrNotFound.
func TestUpdateOnADeadPlaceIsNotFound(t *testing.T) {
	store := &fakeStore{active: false}
	_, err := newService(store).Update(context.Background(), "u1", "p1", "r1", UpdateInput{})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if store.getCalls != 0 {
		t.Errorf("Get called %d times with a dead place, want 0", store.getCalls)
	}
}

// The author reaches the write with the actor intact.
func TestUpdateByTheAuthorPassesTheActor(t *testing.T) {
	rating := 4
	store := &fakeStore{active: true, getReview: Review{ID: "r1", AuthorID: "u1"}, updated: Review{ID: "r1", Rating: 4}}
	review, err := newService(store).Update(context.Background(), "u1", "p1", "r1", UpdateInput{Rating: &rating})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if store.updateCalls != 1 || store.gotActor != "u1" || store.gotReviewID != "r1" {
		t.Errorf("calls=%d actor=%q review=%q", store.updateCalls, store.gotActor, store.gotReviewID)
	}
	if review.Rating != 4 {
		t.Errorf("review = %+v", review)
	}
}

// Moderation is not place-scoped (S12): the admin route addresses the review directly,
// so no place liveness check runs.
func TestModerateHasNoPlaceCheck(t *testing.T) {
	store := &fakeStore{active: false, moderated: Review{ID: "r1", Status: StatusModerated}}
	review, err := newService(store).Moderate(context.Background(), "admin-1", "r1", StatusModerated)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if store.activeCalls != 0 {
		t.Errorf("PlaceActive called %d times from the admin route, want 0", store.activeCalls)
	}
	if store.gotActor != "admin-1" || store.gotStatus != StatusModerated {
		t.Errorf("actor/status = %q/%q", store.gotActor, store.gotStatus)
	}
	if review.Status != StatusModerated {
		t.Errorf("review = %+v", review)
	}
}
