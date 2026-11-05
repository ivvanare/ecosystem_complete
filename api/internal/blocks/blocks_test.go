package blocks

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
)

type fakeStore struct {
	exists  bool
	blocked [][2]string
	removed [][2]string
}

func (f *fakeStore) Block(_ context.Context, blocker, blocked string, _ time.Time) error {
	f.blocked = append(f.blocked, [2]string{blocker, blocked})
	return nil
}
func (f *fakeStore) Unblock(_ context.Context, blocker, blocked string) error {
	f.removed = append(f.removed, [2]string{blocker, blocked})
	return nil
}
func (f *fakeStore) List(context.Context, string) ([]Block, error)    { return nil, nil }
func (f *fakeStore) UserExists(context.Context, string) (bool, error) { return f.exists, nil }

func service(store Store) *Service { return NewService(store, slog.New(slog.DiscardHandler)) }

func TestSelfBlockIsRejectedEverywhere(t *testing.T) {
	store := &fakeStore{exists: true}
	if err := service(store).Block(context.Background(), "u1", "u1"); !errors.Is(err, ErrSelfBlock) {
		t.Fatalf("Block(u1,u1) = %v, want ErrSelfBlock", err)
	}
	if len(store.blocked) != 0 {
		t.Fatal("self-block reached the store")
	}
	if err := service(store).Unblock(context.Background(), "u1", "u1"); !errors.Is(err, ErrSelfBlock) {
		t.Fatalf("Unblock(u1,u1) = %v, want ErrSelfBlock", err)
	}
}

func TestBlockIsIdempotentAndNeedsALiveTarget(t *testing.T) {
	store := &fakeStore{exists: false}
	if err := service(store).Block(context.Background(), "u1", "ghost"); err == nil {
		t.Fatal("blocking an unknown user was accepted")
	}
	if len(store.blocked) != 0 {
		t.Fatal("the store was reached for an unknown target")
	}

	live := &fakeStore{exists: true}
	for i := 0; i < 2; i++ {
		if err := service(live).Block(context.Background(), "u1", "u2"); err != nil {
			t.Fatalf("Block attempt %d: %v", i+1, err)
		}
	}
	// Idempotence lives in the store (ON CONFLICT DO NOTHING); the service must not turn a
	// repeat into an error.
	if len(live.blocked) != 2 {
		t.Fatalf("store saw %d calls, want 2", len(live.blocked))
	}
}

func TestUnblockIsIdempotent(t *testing.T) {
	store := &fakeStore{}
	for i := 0; i < 2; i++ {
		if err := service(store).Unblock(context.Background(), "u1", "u2"); err != nil {
			t.Fatalf("Unblock attempt %d: %v", i+1, err)
		}
	}
}
