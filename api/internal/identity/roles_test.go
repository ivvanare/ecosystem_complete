package identity

import (
	"context"
	"errors"
	"testing"
)

// fakeRoleStore scripts role authoring.
type fakeRoleStore struct {
	role    Role
	users   int
	unknown []string
	err     error
	calls   int
	gotIn   RoleInput
	gotUp   RoleUpdate
	gotCode []string
}

func (f *fakeRoleStore) CreateRole(_ context.Context, _ string, in RoleInput) (Role, error) {
	f.calls++
	f.gotIn = in
	return f.role, f.err
}
func (f *fakeRoleStore) Role(context.Context, string) (Role, error) { return f.role, f.err }
func (f *fakeRoleStore) UpdateRole(_ context.Context, _, _ string, in RoleUpdate) (Role, error) {
	f.calls++
	f.gotUp = in
	return f.role, f.err
}
func (f *fakeRoleStore) RetireRole(context.Context, string, string) error {
	f.calls++
	return f.err
}
func (f *fakeRoleStore) ReplaceRolePermissions(_ context.Context, _, _ string, codes []string) ([]string, error) {
	f.calls++
	f.gotCode = codes
	return codes, f.err
}
func (f *fakeRoleStore) RoleUserCount(context.Context, string) (int, error) { return f.users, f.err }
func (f *fakeRoleStore) UnknownPermissions(context.Context, []string) ([]string, error) {
	return f.unknown, f.err
}

func roleService(store RoleStore) *Service {
	// Identity.New wires the read side; role authoring only needs the seam under test.
	return &Service{RoleAuthor: store}
}

func TestRoleNamesAreTrimmedAndBounded(t *testing.T) {
	store := &fakeRoleStore{role: Role{ID: "r1", Name: "OPERADOR", Status: "ACTIVE"}}
	if _, err := roleService(store).CreateRole(context.Background(), "root",
		RoleInput{Name: "  OPERADOR  "}); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if store.gotIn.Name != "OPERADOR" {
		t.Fatalf("name = %q, want it trimmed", store.gotIn.Name)
	}

	for _, name := range []string{"", "   ", string(make([]byte, 61))} {
		store := &fakeRoleStore{}
		if _, err := roleService(store).CreateRole(context.Background(), "root", RoleInput{Name: name}); !errors.Is(err, ErrInvalidRoleName) {
			t.Fatalf("name %q: err = %v, want ErrInvalidRoleName", name, err)
		}
		if store.calls != 0 {
			t.Fatalf("name %q reached the store", name)
		}
	}
}

func TestAPermissionCodeTheCatalogueLacksIsRefused(t *testing.T) {
	store := &fakeRoleStore{unknown: []string{"inventado"}}
	_, err := roleService(store).ReplaceRolePermissions(context.Background(), "root", "r1", []string{"inventado"})
	var unknown *UnknownPermissionError
	if !errors.As(err, &unknown) {
		t.Fatalf("err = %v, want UnknownPermissionError", err)
	}
	if store.calls != 0 {
		t.Fatal("an unknown code reached the store")
	}
}

// The three doors to the same lockout: renaming the system role, deactivating it, and stripping the
// permission that manages roles. Each is refused before any write.
func TestTheSystemRoleCannotBeLockedOut(t *testing.T) {
	system := &fakeRoleStore{role: Role{ID: "r1", Name: SystemRole, Status: "ACTIVE"}}
	renamed := "OTRO"
	if _, err := roleService(system).UpdateRole(context.Background(), "root", "r1",
		RoleUpdate{Name: &renamed}); !errors.Is(err, ErrRoleProtected) {
		t.Fatalf("rename: err = %v, want ErrRoleProtected", err)
	}
	deactivated := "INACTIVE"
	if _, err := roleService(system).UpdateRole(context.Background(), "root", "r1",
		RoleUpdate{Status: &deactivated}); !errors.Is(err, ErrRoleProtected) {
		t.Fatalf("deactivate: err = %v, want ErrRoleProtected", err)
	}
	if err := roleService(system).RetireRole(context.Background(), "root", "r1"); !errors.Is(err, ErrRoleProtected) {
		t.Fatalf("retire: err = %v, want ErrRoleProtected", err)
	}
	if _, err := roleService(system).ReplaceRolePermissions(context.Background(), "root", "r1",
		[]string{"admin.access"}); !errors.Is(err, ErrRoleProtected) {
		t.Fatalf("strip role.manage: err = %v, want ErrRoleProtected", err)
	}
	if system.calls != 0 {
		t.Fatalf("a protected change reached the store (%d calls)", system.calls)
	}

	// Keeping role.manage is the only shape of that change the system allows.
	allowed := &fakeRoleStore{role: Role{ID: "r1", Name: SystemRole, Status: "ACTIVE"}}
	if _, err := roleService(allowed).ReplaceRolePermissions(context.Background(), "root", "r1",
		[]string{ManagedPermission, "admin.access"}); err != nil {
		t.Fatalf("a set that keeps role.manage was refused: %v", err)
	}
	if allowed.calls != 1 {
		t.Fatal("the allowed change never reached the store")
	}
}

func TestARoleWithUsersIsNotRetired(t *testing.T) {
	store := &fakeRoleStore{role: Role{ID: "r1", Name: "OPERADOR"}, users: 2}
	err := roleService(store).RetireRole(context.Background(), "root", "r1")
	var inUse *RoleInUseError
	if !errors.As(err, &inUse) || inUse.Users != 2 {
		t.Fatalf("err = %v, want RoleInUseError{2}", err)
	}
	if store.calls != 0 {
		t.Fatal("the role was retired over the heads of its users")
	}
}

func TestAnEmptyPermissionSetIsDeliberate(t *testing.T) {
	store := &fakeRoleStore{role: Role{ID: "r1", Name: "OPERADOR"}}
	if _, err := roleService(store).ReplaceRolePermissions(context.Background(), "root", "r1", nil); err != nil {
		t.Fatalf("an empty set was refused: %v", err)
	}
	if store.calls != 1 {
		t.Fatal("the empty set never reached the store")
	}
}
