package identity

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ivvanare/ecosystem_complete/api/internal/audit"
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

// RetireRole mirrors the store's contract: the count and the retire share one transaction there,
// so the refusal is the store's answer, not the service's.
func (f *fakeRoleStore) RetireRole(context.Context, string, string) error {
	f.calls++
	if f.users > 0 {
		return &RoleInUseError{Users: f.users}
	}
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
		RoleInput{Name: "  OPERADOR  "}, audit.Event{}); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if store.gotIn.Name != "OPERADOR" {
		t.Fatalf("name = %q, want it trimmed", store.gotIn.Name)
	}

	for name, want := range map[string]error{
		"":                      ErrRoleNameRequired,
		"   ":                   ErrRoleNameRequired,
		strings.Repeat("a", 61): ErrRoleNameTooLong,
		strings.Repeat("á", 61): ErrRoleNameTooLong,
	} {
		store := &fakeRoleStore{}
		if _, err := roleService(store).CreateRole(context.Background(), "root", RoleInput{Name: name}, audit.Event{}); !errors.Is(err, want) {
			t.Fatalf("name %q: err = %v, want %v", name, err, want)
		}
		if store.calls != 0 {
			t.Fatalf("name %q reached the store", name)
		}
	}

	// Sixty characters of accented letters are sixty characters, not a hundred and twenty bytes.
	accented := strings.Repeat("é", 60)
	accentedStore := &fakeRoleStore{role: Role{ID: "r1", Name: accented, Status: "ACTIVE"}}
	if _, err := roleService(accentedStore).CreateRole(context.Background(), "root", RoleInput{Name: accented}, audit.Event{}); err != nil {
		t.Fatalf("a 60-character name was refused: %v", err)
	}
}

func TestAPermissionCodeTheCatalogueLacksIsRefused(t *testing.T) {
	store := &fakeRoleStore{unknown: []string{"inventado"}}
	_, err := roleService(store).ReplaceRolePermissions(context.Background(), "root", "r1", []string{"inventado"}, audit.Event{})
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
		RoleUpdate{Name: &renamed}, audit.Event{}); !errors.Is(err, ErrRoleProtected) {
		t.Fatalf("rename: err = %v, want ErrRoleProtected", err)
	}
	deactivated := "INACTIVE"
	if _, err := roleService(system).UpdateRole(context.Background(), "root", "r1",
		RoleUpdate{Status: &deactivated}, audit.Event{}); !errors.Is(err, ErrRoleProtected) {
		t.Fatalf("deactivate: err = %v, want ErrRoleProtected", err)
	}
	if err := roleService(system).RetireRole(context.Background(), "root", "r1", audit.Event{}); !errors.Is(err, ErrRoleProtected) {
		t.Fatalf("retire: err = %v, want ErrRoleProtected", err)
	}
	if _, err := roleService(system).ReplaceRolePermissions(context.Background(), "root", "r1",
		[]string{"admin.access"}, audit.Event{}); !errors.Is(err, ErrRoleProtected) {
		t.Fatalf("strip role.manage: err = %v, want ErrRoleProtected", err)
	}
	if system.calls != 0 {
		t.Fatalf("a protected change reached the store (%d calls)", system.calls)
	}

	// Keeping role.manage is the only shape of that change the system allows.
	allowed := &fakeRoleStore{role: Role{ID: "r1", Name: SystemRole, Status: "ACTIVE"}}
	if _, err := roleService(allowed).ReplaceRolePermissions(context.Background(), "root", "r1",
		[]string{ManagedPermission, "admin.access"}, audit.Event{}); err != nil {
		t.Fatalf("a set that keeps role.manage was refused: %v", err)
	}
	if allowed.calls != 1 {
		t.Fatal("the allowed change never reached the store")
	}
}

func TestARoleWithUsersIsNotRetired(t *testing.T) {
	store := &fakeRoleStore{role: Role{ID: "r1", Name: "OPERADOR"}, users: 2}
	err := roleService(store).RetireRole(context.Background(), "root", "r1", audit.Event{})
	var inUse *RoleInUseError
	if !errors.As(err, &inUse) || inUse.Users != 2 {
		t.Fatalf("err = %v, want RoleInUseError{2}", err)
	}
}

func TestAnEmptyPermissionSetIsDeliberate(t *testing.T) {
	store := &fakeRoleStore{role: Role{ID: "r1", Name: "OPERADOR"}}
	if _, err := roleService(store).ReplaceRolePermissions(context.Background(), "root", "r1", nil, audit.Event{}); err != nil {
		t.Fatalf("an empty set was refused: %v", err)
	}
	if store.calls != 1 {
		t.Fatal("the empty set never reached the store")
	}
}

// The role row has no history table, so this event is the only record that a role was renamed,
// deactivated or retired — and of who did it. Without it, editing a role leaves no trace at all.
func TestRoleAuthoringIsRecordedInTheSecurityLog(t *testing.T) {
	store := &fakeRoleStore{role: Role{ID: "r1", Name: "OPERADOR", Status: "ACTIVE",
		Permissions: []string{"admin.access"}}}
	auditor := &fakeAuditor{}
	service := &Service{RoleAuthor: store, Auditor: auditor}

	if _, err := service.CreateRole(context.Background(), "root", RoleInput{Name: "OPERADOR"}, audit.Event{IP: "10.0.0.1"}); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if len(auditor.events) != 1 {
		t.Fatalf("events = %d, want 1", len(auditor.events))
	}
	event := auditor.events[0]
	if event.Name != audit.EventRoleChanged {
		t.Fatalf("name = %q", event.Name)
	}
	if event.ActorID != "root" {
		t.Fatalf("actor = %q, want the operator", event.ActorID)
	}
	if event.UserID != "" {
		t.Fatalf("subject = %q, want empty: the subject is the role, not a user", event.UserID)
	}
	if event.Metadata["action"] != "created" || event.Metadata["coRole"] != "r1" {
		t.Fatalf("metadata = %+v", event.Metadata)
	}
	if event.IP != "10.0.0.1" {
		t.Fatalf("ip = %q, want the request origin", event.IP)
	}
}

func TestEachRoleActionNamesItself(t *testing.T) {
	store := &fakeRoleStore{role: Role{ID: "r1", Name: "OPERADOR", Status: "ACTIVE"}}
	auditor := &fakeAuditor{}
	service := &Service{RoleAuthor: store, Auditor: auditor}

	name := "OTRO"
	if _, err := service.UpdateRole(context.Background(), "root", "r1", RoleUpdate{Name: &name}, audit.Event{}); err != nil {
		t.Fatalf("UpdateRole: %v", err)
	}
	if _, err := service.ReplaceRolePermissions(context.Background(), "root", "r1", []string{"admin.access"}, audit.Event{}); err != nil {
		t.Fatalf("ReplaceRolePermissions: %v", err)
	}
	if err := service.RetireRole(context.Background(), "root", "r1", audit.Event{}); err != nil {
		t.Fatalf("RetireRole: %v", err)
	}

	actions := make([]string, 0, 3)
	for _, event := range auditor.events {
		action, _ := event.Metadata["action"].(string)
		actions = append(actions, action)
	}
	if len(actions) != 3 || actions[0] != "updated" || actions[1] != "permissions_replaced" || actions[2] != "retired" {
		t.Fatalf("actions = %v", actions)
	}
}

func TestARefusedRoleChangeIsNotRecorded(t *testing.T) {
	store := &fakeRoleStore{role: Role{ID: "r1", Name: SystemRole, Status: "ACTIVE"}}
	auditor := &fakeAuditor{}
	service := &Service{RoleAuthor: store, Auditor: auditor}

	_ = service.RetireRole(context.Background(), "root", "r1", audit.Event{})
	if len(auditor.events) != 0 {
		t.Fatalf("a refused change was logged: %+v", auditor.events)
	}
}
