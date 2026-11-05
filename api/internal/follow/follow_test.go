package follow

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
)

type fakeStore struct {
	exists      bool
	existsErr   error
	followErr   error
	unfollowErr error
	page        Page
	listErr     error
	followed    [][2]string
	removed     [][2]string
	direction   string
}

func (f *fakeStore) Follow(_ context.Context, follower, followed string, _ time.Time) error {
	if f.followErr != nil {
		return f.followErr
	}
	f.followed = append(f.followed, [2]string{follower, followed})
	return nil
}

func (f *fakeStore) Unfollow(_ context.Context, follower, followed string) error {
	if f.unfollowErr != nil {
		return f.unfollowErr
	}
	f.removed = append(f.removed, [2]string{follower, followed})
	return nil
}

func (f *fakeStore) UserExists(context.Context, string) (bool, error) { return f.exists, f.existsErr }

func (f *fakeStore) List(_ context.Context, _, direction string, _, _ int) (Page, error) {
	f.direction = direction
	return f.page, f.listErr
}

func service(store Store) *Service { return NewService(store, slog.New(slog.DiscardHandler)) }

func TestFollowRejectsSelfFollow(t *testing.T) {
	store := &fakeStore{exists: true}
	err := service(store).Follow(context.Background(), "u1", "u1")
	if !errors.Is(err, ErrSelfFollow) {
		t.Fatalf("Follow(u1,u1) = %v, want ErrSelfFollow", err)
	}
	if len(store.followed) != 0 {
		t.Fatalf("self-follow reached the store: %v", store.followed)
	}
}

func TestFollowRejectsUnknownTarget(t *testing.T) {
	store := &fakeStore{exists: false}
	err := service(store).Follow(context.Background(), "u1", "ghost")
	if !errors.Is(err, ErrUnknownUser) {
		t.Fatalf("Follow(unknown) = %v, want ErrUnknownUser", err)
	}
}

func TestFollowIsIdempotentAndRecordsTheEdge(t *testing.T) {
	store := &fakeStore{exists: true}
	svc := service(store)
	for i := 0; i < 2; i++ {
		if err := svc.Follow(context.Background(), "u1", "u2"); err != nil {
			t.Fatalf("Follow attempt %d: %v", i+1, err)
		}
	}
	// Idempotence lives in the store (ON CONFLICT DO NOTHING); the service must not turn a
	// repeat into an error.
	if len(store.followed) != 2 {
		t.Fatalf("store saw %d calls, want 2", len(store.followed))
	}
}

func TestUnfollowIsIdempotentAndRejectsSelf(t *testing.T) {
	store := &fakeStore{}
	svc := service(store)
	if err := svc.Unfollow(context.Background(), "u1", "u1"); !errors.Is(err, ErrSelfFollow) {
		t.Fatalf("Unfollow(u1,u1) = %v, want ErrSelfFollow", err)
	}
	for i := 0; i < 2; i++ {
		if err := svc.Unfollow(context.Background(), "u1", "u2"); err != nil {
			t.Fatalf("Unfollow attempt %d: %v", i+1, err)
		}
	}
}

func TestListDefaultsToFollowingAndRejectsUnknownDirection(t *testing.T) {
	store := &fakeStore{page: Page{Total: 1}}
	svc := service(store)

	if _, err := svc.List(context.Background(), "u1", "", 1, 20); err != nil {
		t.Fatalf("List: %v", err)
	}
	if store.direction != DirectionFollowing {
		t.Fatalf("default direction = %q, want following", store.direction)
	}
	if _, err := svc.List(context.Background(), "u1", "enemies", 1, 20); err == nil {
		t.Fatal("List with an unknown direction returned nil error")
	}
}

func TestFollowPropagatesStoreFailure(t *testing.T) {
	store := &fakeStore{exists: true, followErr: errors.New("db down")}
	if err := service(store).Follow(context.Background(), "u1", "u2"); err == nil {
		t.Fatal("Follow returned nil error on a store failure")
	}
}
