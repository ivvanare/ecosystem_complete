package identity

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/audit"
)

type fakeAdmin struct {
	users    Page[UserSummary]
	roles    []string
	setErr   error
	events   Page[AuditEvent]
	gotRoles []string
	gotUser  string
}

func (f *fakeAdmin) ListUsers(context.Context, UserFilter) (Page[UserSummary], error) {
	return f.users, nil
}

func (f *fakeAdmin) SetRoles(_ context.Context, userID string, roles []string) ([]string, error) {
	f.gotUser = userID
	f.gotRoles = roles
	if f.setErr != nil {
		return nil, f.setErr
	}
	return f.roles, nil
}

func (f *fakeAdmin) ListAuditEvents(context.Context, AuditFilter) (Page[AuditEvent], error) {
	return f.events, nil
}

type fakeAuditor struct {
	events []audit.Event
}

func (f *fakeAuditor) Record(_ context.Context, event audit.Event) error {
	f.events = append(f.events, event)
	return nil
}

func TestSetRolesAuditsActorAndSubject(t *testing.T) {
	admin := &fakeAdmin{roles: []string{"EDITOR"}}
	auditor := &fakeAuditor{}
	service := New(&fakeStore{}, nil)
	service.Admin = admin
	service.Auditor = auditor
	service.Logger = slog.New(slog.DiscardHandler)

	roles, err := service.SetRoles(context.Background(), "admin-1", "user-2", []string{"EDITOR"},
		audit.Event{IP: "10.0.0.9", UserAgent: "k6"})
	if err != nil {
		t.Fatalf("SetRoles returned error: %v", err)
	}
	if len(roles) != 1 || roles[0] != "EDITOR" {
		t.Errorf("roles = %v", roles)
	}

	if len(auditor.events) != 1 {
		t.Fatalf("audited %d events, want 1", len(auditor.events))
	}
	event := auditor.events[0]
	if event.Name != audit.EventRolesChanged {
		t.Errorf("event = %q", event.Name)
	}
	if event.UserID != "user-2" {
		t.Errorf("subject = %q, want the user whose roles changed", event.UserID)
	}
	if event.ActorID != "admin-1" {
		t.Errorf("actor = %q, want the administrator", event.ActorID)
	}
	if event.IP != "10.0.0.9" || event.UserAgent != "k6" {
		t.Errorf("client context lost: %+v", event)
	}
	if roles, ok := event.Metadata["roles"].([]string); !ok || len(roles) != 1 {
		t.Errorf("metadata = %v", event.Metadata)
	}
}

func TestSetRolesDoesNotAuditAFailedChange(t *testing.T) {
	admin := &fakeAdmin{setErr: ErrUnknownRole}
	auditor := &fakeAuditor{}
	service := New(&fakeStore{}, nil)
	service.Admin = admin
	service.Auditor = auditor
	service.Logger = slog.New(slog.DiscardHandler)

	if _, err := service.SetRoles(context.Background(), "admin-1", "user-2", []string{"NADA"}, audit.Event{}); !errors.Is(err, ErrUnknownRole) {
		t.Fatalf("error = %v, want ErrUnknownRole", err)
	}
	if len(auditor.events) != 0 {
		t.Error("a rejected change was audited as if it had happened")
	}
}

func TestValidateRolesRejectsDuplicatesAndBlanks(t *testing.T) {
	if err := validateRoles([]string{"EDITOR", "EDITOR"}); !errors.Is(err, ErrUnknownRole) {
		t.Errorf("duplicates: error = %v", err)
	}
	if err := validateRoles([]string{""}); !errors.Is(err, ErrUnknownRole) {
		t.Errorf("blank: error = %v", err)
	}
	if err := validateRoles(nil); err != nil {
		t.Errorf("empty set should be allowed (it removes every role): %v", err)
	}
}

func TestNormalizePaging(t *testing.T) {
	cases := []struct{ page, limit, wantPage, wantLimit int }{
		{0, 0, 1, 20},
		{-3, -1, 1, 20},
		{2, 500, 2, 100},
		{5, 10, 5, 10},
	}
	for _, tc := range cases {
		page, limit := normalizePaging(tc.page, tc.limit)
		if page != tc.wantPage || limit != tc.wantLimit {
			t.Errorf("normalizePaging(%d,%d) = %d,%d; want %d,%d", tc.page, tc.limit, page, limit, tc.wantPage, tc.wantLimit)
		}
	}
	if offset(3, 20) != 40 {
		t.Errorf("offset(3,20) = %d, want 40", offset(3, 20))
	}
}

func TestUserBuildsAListRow(t *testing.T) {
	name, email := "Ana", "ana@melia.test"
	verified := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	service := New(&fakeStore{
		profile: Profile{ID: "user-1", Name: &name, Email: &email, Status: "ACTIVE", EmailVerifiedAt: &verified},
		access:  Access{Roles: []string{"ADMIN"}},
	}, nil)

	user, err := service.User(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("User returned error: %v", err)
	}
	if user.ID != "user-1" || len(user.Roles) != 1 || user.Roles[0] != "ADMIN" {
		t.Errorf("unexpected row: %+v", user)
	}
}
