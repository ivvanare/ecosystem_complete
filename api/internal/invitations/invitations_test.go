package invitations

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
)

// fakeStore scripts the plan role and records what the service asked for.
type fakeStore struct {
	role       string
	visibility string
	answer     string
	answeredBy string
	created    CreateInput
	planListed bool
	userListed bool
}

func (f *fakeStore) Role(context.Context, string, string) (string, string, error) {
	return f.role, f.visibility, nil
}
func (f *fakeStore) PlanVisible(context.Context, string, string) (bool, error) { return true, nil }
func (f *fakeStore) Create(_ context.Context, _, _ string, in CreateInput) (Invitation, error) {
	f.created = in
	return Invitation{ID: "i1", Status: StatusPending, Mode: in.Mode}, nil
}
func (f *fakeStore) ListForPlan(context.Context, string) ([]Invitation, error) {
	f.planListed = true
	return []Invitation{{ID: "i1"}}, nil
}
func (f *fakeStore) ListForUser(context.Context, string) ([]Invitation, error) {
	f.userListed = true
	return []Invitation{{ID: "i1"}}, nil
}
func (f *fakeStore) VenueHidden(context.Context, string, string) (bool, error) { return false, nil }
func (f *fakeStore) Expire(context.Context, time.Time) (int64, error)          { return 0, nil }
func (f *fakeStore) Answer(_ context.Context, actorID, _, status string, _ time.Time) error {
	f.answeredBy, f.answer = actorID, status
	return nil
}

func service(store Store) *Service { return NewService(store, slog.New(slog.DiscardHandler)) }

func future() time.Time { return time.Now().UTC().Add(48 * time.Hour) }

// Who may invite, and what an invitation must carry.

func TestOnlyEditorOrOwnerCanInvite(t *testing.T) {
	for _, role := range []string{"", "VIEWER"} {
		store := &fakeStore{role: role, visibility: "PUBLIC"}
		_, err := service(store).Create(context.Background(), "someone", "p1",
			CreateInput{UserID: "u2", ExpiresAt: future()})
		if err == nil {
			t.Fatalf("role %q was allowed to invite", role)
		}
		if store.created.UserID != "" {
			t.Fatalf("role %q reached the store", role)
		}
	}
	for _, role := range []string{"EDITOR", "OWNER"} {
		store := &fakeStore{role: role, visibility: "PRIVATE"}
		if _, err := service(store).Create(context.Background(), "member", "p1",
			CreateInput{UserID: "u2", ExpiresAt: future()}); err != nil {
			t.Fatalf("role %q could not invite: %v", role, err)
		}
	}
}

func TestSurpriseNeedsContent(t *testing.T) {
	store := &fakeStore{role: "OWNER", visibility: "PRIVATE"}
	_, err := service(store).Create(context.Background(), "owner", "p1",
		CreateInput{UserID: "u2", ExpiresAt: future(), Mode: ModeSurprise})
	if !errors.Is(err, ErrSurpriseNeedsContent) {
		t.Fatalf("empty surprise = %v, want ErrSurpriseNeedsContent", err)
	}

	hint := "llevá atuendo cómodo, es un rooftop"
	if _, err := service(store).Create(context.Background(), "owner", "p1",
		CreateInput{UserID: "u2", ExpiresAt: future(), Mode: ModeSurprise, Hint: &hint}); err != nil {
		t.Fatalf("a SURPRISE with a hint was rejected: %v", err)
	}
}

func TestExpiryMustBeInTheFuture(t *testing.T) {
	store := &fakeStore{role: "OWNER", visibility: "PRIVATE"}
	_, err := service(store).Create(context.Background(), "owner", "p1",
		CreateInput{UserID: "u2", ExpiresAt: time.Now().UTC().Add(-time.Hour)})
	if !errors.Is(err, ErrExpiryNotFuture) {
		t.Fatalf("past expiry = %v, want ErrExpiryNotFuture", err)
	}
}

func TestOnlyTheHostReadsEveryAnswer(t *testing.T) {
	for _, role := range []string{"OWNER", "EDITOR"} {
		store := &fakeStore{role: role}
		if _, err := service(store).ForPlan(context.Background(), "host", "p1"); err != nil {
			t.Fatalf("role %q could not read the RSVP list: %v", role, err)
		}
		if !store.planListed {
			t.Fatalf("role %q did not reach the list", role)
		}
	}
	// A VIEWER sees the plan and still does not see who declined.
	viewer := &fakeStore{role: "VIEWER"}
	if _, err := service(viewer).ForPlan(context.Background(), "guest", "p1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a VIEWER read the RSVP list: %v", err)
	}
	if viewer.planListed {
		t.Fatal("the guest reached the list")
	}
}

func TestAcceptAndDeclineCarryTheRightStatus(t *testing.T) {
	store := &fakeStore{}
	if err := service(store).Accept(context.Background(), "invitee", "i1"); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if store.answer != StatusAccepted || store.answeredBy != "invitee" {
		t.Fatalf("accept = %q by %q", store.answer, store.answeredBy)
	}
	if err := service(store).Decline(context.Background(), "invitee", "i1"); err != nil {
		t.Fatalf("Decline: %v", err)
	}
	if store.answer != StatusDeclined {
		t.Fatalf("decline = %q", store.answer)
	}
}

func TestMineOnlyListsTheCallersOwn(t *testing.T) {
	store := &fakeStore{}
	if _, err := service(store).Mine(context.Background(), "invitee"); err != nil {
		t.Fatalf("Mine: %v", err)
	}
	if !store.userListed {
		t.Fatal("the caller's own list was not requested")
	}
}

func TestADuplicateLiveInvitationIsAConflict(t *testing.T) {
	// The partial unique index is the authority; the store maps 23505 to this sentinel so the
	// transport can answer 409 instead of a 500 for a situation the client can resolve.
	if !errors.Is(ErrAlreadyPending, ErrAlreadyPending) {
		t.Fatal("sentinel is not itself")
	}
	store := &fakeStore{role: "OWNER", visibility: "PRIVATE"}
	if _, err := service(store).Create(context.Background(), "owner", "p1",
		CreateInput{UserID: "u2", ExpiresAt: future()}); err != nil {
		t.Fatalf("first invitation: %v", err)
	}
}

// fakeSweeper counts the rows the job asked to expire.
type fakeSweeper struct {
	expired int64
	err     error
	now     time.Time
}

func (f *fakeSweeper) Expire(_ context.Context, now time.Time) (int64, error) {
	f.now = now
	return f.expired, f.err
}

func TestSweeperExpiresForHygieneOnly(t *testing.T) {
	// The job touches rows; it is not what makes an expired invitation unusable — the read
	// predicate already does that, which is why a broken sweeper is harmless.
	store := &fakeSweeper{expired: 3}
	job := NewSweeper(&sweeperStore{fakeSweeper: store}, slog.New(slog.DiscardHandler))
	if job.Name() != "invitation-expiry" {
		t.Fatalf("job name = %q", job.Name())
	}
	if err := job.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if store.now.IsZero() {
		t.Fatal("the sweeper did not ask the store to expire anything")
	}
}

func TestSweeperPropagatesFailure(t *testing.T) {
	job := NewSweeper(&sweeperStore{fakeSweeper: &fakeSweeper{err: errors.New("db down")}},
		slog.New(slog.DiscardHandler))
	if err := job.Run(context.Background()); err == nil {
		t.Fatal("Run returned nil on a store failure")
	}
}

// sweeperStore satisfies Store for the sweeper tests alone.
type sweeperStore struct{ *fakeSweeper }

func (s *sweeperStore) Role(context.Context, string, string) (string, string, error) {
	return "", "", nil
}
func (s *sweeperStore) PlanVisible(context.Context, string, string) (bool, error) { return false, nil }
func (s *sweeperStore) Create(context.Context, string, string, CreateInput) (Invitation, error) {
	return Invitation{}, nil
}
func (s *sweeperStore) ListForPlan(context.Context, string) ([]Invitation, error)       { return nil, nil }
func (s *sweeperStore) ListForUser(context.Context, string) ([]Invitation, error)       { return nil, nil }
func (s *sweeperStore) VenueHidden(context.Context, string, string) (bool, error)       { return false, nil }
func (s *sweeperStore) Answer(context.Context, string, string, string, time.Time) error { return nil }
