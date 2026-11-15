package profile

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeStore struct {
	profile Profile
	err     error
	seen    string
}

func (f *fakeStore) Profile(_ context.Context, userID string) (Profile, error) {
	f.seen = userID
	return f.profile, f.err
}

func TestTheProfileIsPassedThrough(t *testing.T) {
	name := "Ana"
	since := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	store := &fakeStore{profile: Profile{
		ID: "u1", Name: &name, MemberSince: since,
		Counts: Counts{Reviews: 3, Checkins: 12, Followers: 5, Following: 8},
	}}

	account, err := New(store).Profile(context.Background(), "u1")
	if err != nil {
		t.Fatalf("Profile: %v", err)
	}
	if store.seen != "u1" || account.Counts.Followers != 5 || account.Name == nil || *account.Name != "Ana" {
		t.Fatalf("account = %+v", account)
	}
	if !account.MemberSince.Equal(since) {
		t.Fatalf("memberSince = %v", account.MemberSince)
	}
}

// An account that never gave a name is a real state (social sign-in), not an error.
func TestAnAccountWithoutANameIsStillAProfile(t *testing.T) {
	account, err := New(&fakeStore{profile: Profile{ID: "u1"}}).Profile(context.Background(), "u1")
	if err != nil {
		t.Fatalf("Profile: %v", err)
	}
	if account.Name != nil {
		t.Fatalf("name = %v, want nil", *account.Name)
	}
}

// Suspended, deleted and absent are one answer: a stranger cannot tell them apart.
func TestAnInvisibleAccountIsNotFound(t *testing.T) {
	_, err := New(&fakeStore{err: ErrNotFound}).Profile(context.Background(), "u1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// A real failure keeps its cause, so the log says what broke.
func TestAFailingReadIsWrapped(t *testing.T) {
	cause := errors.New("db down")
	_, err := New(&fakeStore{err: cause}).Profile(context.Background(), "u1")
	if !errors.Is(err, cause) {
		t.Fatalf("err = %v, want the cause to survive", err)
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatal("a failure was reported as not found")
	}
}
