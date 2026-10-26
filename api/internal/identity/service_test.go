package identity

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeStore struct {
	profile Profile
	access  Access
	err     error
	lookups int
}

func (f *fakeStore) Profile(context.Context, string) (Profile, error) {
	f.lookups++
	if f.err != nil {
		return Profile{}, f.err
	}
	return f.profile, nil
}

func (f *fakeStore) Access(context.Context, string) (Access, error) {
	if f.err != nil {
		return Access{}, f.err
	}
	return f.access, nil
}

func TestMeMergesProfileAndLiveAccess(t *testing.T) {
	name := "Ana"
	email := "ana@melia.test"
	verified := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)

	store := &fakeStore{
		profile: Profile{ID: "user-1", Name: &name, Email: &email, Status: "ACTIVE", EmailVerifiedAt: &verified},
		access:  Access{Roles: []string{"EDITOR"}, Permissions: []string{"admin.access", "place.create"}},
	}

	me, err := New(store, nil).Me(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("Me returned error: %v", err)
	}
	if me.ID != "user-1" || *me.Email != email {
		t.Errorf("profile not carried: %+v", me.Profile)
	}
	if len(me.Roles) != 1 || me.Roles[0] != "EDITOR" {
		t.Errorf("roles = %v", me.Roles)
	}
	if len(me.Permissions) != 2 {
		t.Errorf("permissions = %v", me.Permissions)
	}
}

func TestMePropagatesNotFound(t *testing.T) {
	_, err := New(&fakeStore{err: ErrNotFound}, nil).Me(context.Background(), "fantasma")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestCanReadsTheDatabaseEveryTime(t *testing.T) {
	store := &fakeStore{access: Access{Permissions: []string{"admin.access"}}}
	service := New(store, nil)

	allowed, err := service.Can(context.Background(), "user-1", "admin.access")
	if err != nil {
		t.Fatalf("Can returned error: %v", err)
	}
	if !allowed {
		t.Error("Can denied a held permission")
	}

	denied, err := service.Can(context.Background(), "user-1", "role.manage")
	if err != nil {
		t.Fatalf("Can returned error: %v", err)
	}
	if denied {
		t.Error("Can granted a permission the user does not hold")
	}

	// A revoked role must be visible on the very next call, which is why nothing is cached in
	// the token.
	store.access = Access{Permissions: nil}
	afterRevocation, err := service.Can(context.Background(), "user-1", "admin.access")
	if err != nil {
		t.Fatalf("Can returned error: %v", err)
	}
	if afterRevocation {
		t.Error("a revoked permission still passes")
	}
}
