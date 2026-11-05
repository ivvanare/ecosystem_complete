package plans

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

// fakeStore scripts roles and records what the service asked for.
type fakeStore struct {
	role       string
	visibility string
	roleErr    error
	joined     string // role the service chose for the self-join
	removed    string
	created    bool
	updated    UpdateInput
}

func (f *fakeStore) Create(_ context.Context, _ string, _ CreateInput) (Plan, error) {
	f.created = true
	return Plan{ID: "p1", ViewerRole: RoleOwner, Visibility: VisibilityPrivate}, nil
}
func (f *fakeStore) Get(context.Context, string, string) (Plan, error) { return Plan{ID: "p1"}, nil }
func (f *fakeStore) Role(context.Context, string, string) (string, string, error) {
	return f.role, f.visibility, f.roleErr
}
func (f *fakeStore) Update(_ context.Context, _, _ string, in UpdateInput) (Plan, error) {
	f.updated = in
	return Plan{ID: "p1"}, nil
}
func (f *fakeStore) Join(_ context.Context, _, _, role string) error  { f.joined = role; return nil }
func (f *fakeStore) Remove(_ context.Context, _, userID string) error { f.removed = userID; return nil }
func (f *fakeStore) ListFor(context.Context, string, int, int) ([]Plan, int, error) {
	return []Plan{{ID: "p1"}}, 1, nil
}

func service(store Store) *Service { return NewService(store, slog.New(slog.DiscardHandler)) }

// The rules that make a plan's door mean something. They live here, not in the transport.

func TestCreateDefaultsToPrivate(t *testing.T) {
	store := &fakeStore{}
	if _, err := service(store).Create(context.Background(), "owner-1", CreateInput{Name: "Cena"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !store.created {
		t.Fatal("the store was never asked to insert the plan")
	}
}

func TestCreateRejectsUnknownVisibility(t *testing.T) {
	if _, err := service(&fakeStore{}).Create(context.Background(), "o", CreateInput{Name: "X", Visibility: "SECRET"}); err == nil {
		t.Fatal("an unknown visibility was accepted")
	}
}

func TestSelfJoinOnAPublicPlanGrantsViewerNeverEditor(t *testing.T) {
	// The single most important guard of this slice (D8): an open door that hands out EDITOR
	// is a plan anyone can rearrange and empty.
	store := &fakeStore{role: "", visibility: VisibilityPublic}
	if err := service(store).Join(context.Background(), "guest", "p1"); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if store.joined != RoleViewer {
		t.Fatalf("self-join granted %q, want VIEWER", store.joined)
	}
}

func TestSelfJoinOnAPrivatePlanIsNotFound(t *testing.T) {
	store := &fakeStore{role: "", visibility: VisibilityPrivate}
	err := service(store).Join(context.Background(), "stranger", "p1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Join on a private plan = %v, want ErrNotFound", err)
	}
	if store.joined != "" {
		t.Fatalf("a membership was created on a private plan: %q", store.joined)
	}
}

func TestJoiningTwiceDoesNotDemoteAnExistingRole(t *testing.T) {
	store := &fakeStore{role: RoleEditor, visibility: VisibilityPublic}
	if err := service(store).Join(context.Background(), "editor", "p1"); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if store.joined != "" {
		t.Fatalf("an existing EDITOR was re-joined as %q", store.joined)
	}
}

func TestViewerCannotUpdateAVisiblePlan(t *testing.T) {
	store := &fakeStore{role: RoleViewer, visibility: VisibilityPublic}
	name := "otro nombre"
	_, err := service(store).Update(context.Background(), "viewer", "p1", UpdateInput{Name: &name})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Update by a VIEWER = %v, want ErrForbidden", err)
	}
	if store.updated.Name != nil {
		t.Fatal("the update reached the store")
	}
}

func TestStrangerReadingAPrivatePlanIsNotFound(t *testing.T) {
	store := &fakeStore{role: "", visibility: VisibilityPrivate}
	if _, err := service(store).Update(context.Background(), "stranger", "p1", UpdateInput{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Update on a private plan by a stranger = %v, want ErrNotFound", err)
	}
}

func TestOnlyTheOwnerChangesVisibility(t *testing.T) {
	public := VisibilityPublic
	store := &fakeStore{role: RoleEditor, visibility: VisibilityPrivate}
	if _, err := service(store).Update(context.Background(), "editor", "p1", UpdateInput{Visibility: &public}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("an EDITOR changed visibility, want ErrForbidden (got %v)", err)
	}

	ownerStore := &fakeStore{role: RoleOwner, visibility: VisibilityPrivate}
	if _, err := service(ownerStore).Update(context.Background(), "owner", "p1", UpdateInput{Visibility: &public}); err != nil {
		t.Fatalf("the OWNER could not publish: %v", err)
	}
}

func TestEditorUpdatesTheRestOfThePlan(t *testing.T) {
	store := &fakeStore{role: RoleEditor, visibility: VisibilityPrivate}
	name := "Nuevo nombre"
	if _, err := service(store).Update(context.Background(), "editor", "p1", UpdateInput{Name: &name}); err != nil {
		t.Fatalf("an EDITOR could not rename: %v", err)
	}
	if store.updated.Name == nil || *store.updated.Name != name {
		t.Fatalf("update not applied: %+v", store.updated.Name)
	}
}

func TestOwnerCannotLeaveAlone(t *testing.T) {
	store := &fakeStore{role: RoleOwner, visibility: VisibilityPrivate}
	if err := service(store).Leave(context.Background(), "owner", "p1", "owner"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("owner leaving alone = %v, want ErrForbidden", err)
	}
	if store.removed != "" {
		t.Fatal("the membership was removed anyway")
	}
}

func TestMemberLeavesOnTheirOwn(t *testing.T) {
	store := &fakeStore{role: RoleViewer, visibility: VisibilityPublic}
	if err := service(store).Leave(context.Background(), "guest", "p1", "guest"); err != nil {
		t.Fatalf("Leave: %v", err)
	}
	if store.removed != "guest" {
		t.Fatalf("removed %q, want the caller", store.removed)
	}
}

func TestOwnerRemovesSomeoneElseButNotAStrangerCaller(t *testing.T) {
	owner := &fakeStore{role: RoleOwner, visibility: VisibilityPrivate}
	if err := service(owner).Leave(context.Background(), "owner", "p1", "guest"); err != nil {
		t.Fatalf("owner removing a member: %v", err)
	}
	if owner.removed != "guest" {
		t.Fatalf("removed %q", owner.removed)
	}

	viewer := &fakeStore{role: RoleViewer, visibility: VisibilityPublic}
	if err := service(viewer).Leave(context.Background(), "viewer", "p1", "other"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a VIEWER removed someone else: %v", err)
	}
}
