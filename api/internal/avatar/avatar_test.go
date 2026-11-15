package avatar

import (
	"context"
	"errors"
	"testing"
)

type fakeStore struct {
	previous *string
	err      error
	url      string
	cleared  bool
}

func (f *fakeStore) Set(_ context.Context, _, url string) (*string, error) {
	f.url = url
	return f.previous, f.err
}

func (f *fakeStore) Clear(context.Context, string) (*string, error) {
	f.cleared = true
	return f.previous, f.err
}

// Replacing an avatar reports the URL it replaced: that is what lets the caller delete the object
// instead of leaving it behind, and an avatar is the one upload a user repeats.
func TestSetReportsTheReplacedURL(t *testing.T) {
	previous := "/media/avatars/u1/1.png"
	store := &fakeStore{previous: &previous}

	got, err := New(store).Set(context.Background(), "u1", "/media/avatars/u1/2.png")
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got == nil || *got != previous {
		t.Fatalf("previous = %v, want %q", got, previous)
	}
	if store.url != "/media/avatars/u1/2.png" {
		t.Fatalf("stored %q", store.url)
	}
}

// A first avatar replaces nothing, and nil is the honest answer.
func TestTheFirstAvatarReplacesNothing(t *testing.T) {
	got, err := New(&fakeStore{}).Set(context.Background(), "u1", "/media/avatars/u1/1.png")
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got != nil {
		t.Fatalf("previous = %v, want nil", *got)
	}
}

func TestClearReportsTheRemovedURL(t *testing.T) {
	previous := "/media/avatars/u1/1.png"
	store := &fakeStore{previous: &previous}
	got, err := New(store).Clear(context.Background(), "u1")
	if err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if got == nil || *got != previous || !store.cleared {
		t.Fatalf("got = %v, cleared = %v", got, store.cleared)
	}
}

// A valid token for an account that is gone is an authentication problem, and it keeps its name
// so the transport can answer 401 instead of a generic 500.
func TestAGoneAccountIsNotFound(t *testing.T) {
	_, err := New(&fakeStore{err: ErrNotFound}).Set(context.Background(), "u1", "/x.png")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	_, err = New(&fakeStore{err: ErrNotFound}).Clear(context.Background(), "u1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("clear err = %v, want ErrNotFound", err)
	}
}

func TestARealFailureKeepsItsCause(t *testing.T) {
	cause := errors.New("db down")
	_, err := New(&fakeStore{err: cause}).Set(context.Background(), "u1", "/x.png")
	if !errors.Is(err, cause) {
		t.Fatalf("err = %v, want the cause to survive", err)
	}
}
