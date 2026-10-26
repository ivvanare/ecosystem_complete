package login

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/audit"
	"github.com/ivvanare/ecosystem_complete/api/internal/session"
)

type fakeStore struct {
	user      User
	lookupErr error
	failures  int
	lockSet   *time.Time
	resets    int
}

func (f *fakeStore) ByEmail(context.Context, string) (User, error) {
	if f.lookupErr != nil {
		return User{}, f.lookupErr
	}
	return f.user, nil
}

func (f *fakeStore) RecordFailure(_ context.Context, _ string, lockUntil *time.Time) error {
	f.failures++
	f.lockSet = lockUntil
	return nil
}

func (f *fakeStore) ResetFailures(context.Context, string) error {
	f.resets++
	return nil
}

type fakeIssuer struct {
	calls int
	err   error
}

func (f *fakeIssuer) Issue(context.Context, string, session.Meta) (session.Tokens, error) {
	f.calls++
	if f.err != nil {
		return session.Tokens{}, f.err
	}
	return session.Tokens{AccessToken: "access", RefreshToken: "refresh"}, nil
}

type fakeAuditor struct {
	events []audit.Event
	err    error
}

func (f *fakeAuditor) Record(_ context.Context, event audit.Event) error {
	f.events = append(f.events, event)
	return f.err
}

func (f *fakeAuditor) names() []string {
	names := make([]string, 0, len(f.events))
	for _, e := range f.events {
		names = append(names, e.Name)
	}
	return names
}

var loginNow = time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)

func activeUser() User {
	return User{
		ID:           "user-1",
		Email:        "ana@melia.test",
		PasswordHash: "argon2id$hash",
		Status:       "ACTIVE",
	}
}

func newService(store Store, issuer Issuer, auditor audit.Recorder) *Service {
	service := New(store, issuer, auditor, slog.New(slog.DiscardHandler))
	service.Now = func() time.Time { return loginNow }
	service.VerifyPassword = func(encoded, plain string) (bool, error) {
		return encoded == "argon2id$hash" && plain == "correcta", nil
	}
	return service
}

func TestLoginSucceedsAndAuditsBothEvents(t *testing.T) {
	store := &fakeStore{user: activeUser()}
	issuer := &fakeIssuer{}
	auditor := &fakeAuditor{}
	service := newService(store, issuer, auditor)

	tokens, err := service.Login(context.Background(), " Ana@Melia.test ", "correcta", session.Meta{IP: "10.0.0.1"})
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	if tokens.AccessToken == "" {
		t.Error("no tokens issued")
	}
	if store.resets != 1 {
		t.Errorf("resets = %d, want 1", store.resets)
	}
	if got := auditor.names(); len(got) != 2 || got[0] != audit.EventLoginSucceeded || got[1] != audit.EventSessionCreated {
		t.Errorf("audit events = %v, want login.succeeded + session.created", got)
	}
	if auditor.events[0].IP != "10.0.0.1" {
		t.Errorf("audit lost the client IP: %+v", auditor.events[0])
	}
}

func TestLoginWithAWrongPasswordCountsWithoutLocking(t *testing.T) {
	store := &fakeStore{user: activeUser()}
	issuer := &fakeIssuer{}
	auditor := &fakeAuditor{}
	service := newService(store, issuer, auditor)

	_, err := service.Login(context.Background(), "ana@melia.test", "incorrecta", session.Meta{})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("error = %v, want ErrInvalidCredentials", err)
	}
	if store.failures != 1 {
		t.Errorf("failures = %d, want 1", store.failures)
	}
	if store.lockSet != nil {
		t.Error("the account was locked before reaching the threshold")
	}
	if issuer.calls != 0 {
		t.Error("tokens were issued with a wrong password")
	}
	if got := auditor.names(); len(got) != 1 || got[0] != audit.EventLoginFailed {
		t.Errorf("audit events = %v, want one login.failed", got)
	}
}

func TestLoginLocksAtTheThreshold(t *testing.T) {
	user := activeUser()
	user.FailedAttempts = DefaultMaxAttempts - 1
	store := &fakeStore{user: user}
	auditor := &fakeAuditor{}
	service := newService(store, &fakeIssuer{}, auditor)

	_, err := service.Login(context.Background(), "ana@melia.test", "incorrecta", session.Meta{})

	var locked *LockedError
	if !errors.As(err, &locked) {
		t.Fatalf("error = %v, want *LockedError", err)
	}
	if locked.RetryAfter != DefaultLockDuration {
		t.Errorf("RetryAfter = %v, want %v", locked.RetryAfter, DefaultLockDuration)
	}
	if store.lockSet == nil || !store.lockSet.Equal(loginNow.Add(DefaultLockDuration)) {
		t.Errorf("lockUntil = %v, want now+15m", store.lockSet)
	}
	if got := auditor.names(); len(got) != 1 || got[0] != audit.EventLoginLocked {
		t.Errorf("audit events = %v, want one login.locked", got)
	}
}

func TestLoginRejectsALockedAccountWithoutVerifying(t *testing.T) {
	user := activeUser()
	until := loginNow.Add(5 * time.Minute)
	user.LockedUntil = &until
	store := &fakeStore{user: user}
	service := newService(store, &fakeIssuer{}, &fakeAuditor{})

	verified := false
	service.VerifyPassword = func(string, string) (bool, error) { verified = true; return true, nil }

	_, err := service.Login(context.Background(), "ana@melia.test", "correcta", session.Meta{})

	var locked *LockedError
	if !errors.As(err, &locked) {
		t.Fatalf("error = %v, want *LockedError", err)
	}
	if locked.RetryAfter != 5*time.Minute {
		t.Errorf("RetryAfter = %v, want 5m", locked.RetryAfter)
	}
	if verified {
		t.Error("a locked account still paid for a password verification")
	}
}

func TestLoginForAnUnknownAddressPaysTheSameWorkAndLeaksNothing(t *testing.T) {
	store := &fakeStore{lookupErr: ErrNotFound}
	auditor := &fakeAuditor{}
	service := newService(store, &fakeIssuer{}, auditor)

	verifications := 0
	service.VerifyPassword = func(string, string) (bool, error) {
		verifications++
		return false, nil
	}

	_, err := service.Login(context.Background(), "nadie@melia.test", "lo-que-sea", session.Meta{})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("error = %v, want ErrInvalidCredentials", err)
	}
	if verifications != 1 {
		t.Errorf("verifications = %d, want 1: a miss must cost the same as a hit", verifications)
	}
	if got := auditor.names(); len(got) != 1 || got[0] != audit.EventLoginFailed {
		t.Errorf("audit events = %v", got)
	}
	if reason, _ := auditor.events[0].Metadata["reason"].(string); reason != "unknown_account" {
		t.Errorf("reason = %v, want unknown_account", auditor.events[0].Metadata["reason"])
	}
}

func TestLoginRejectsAnInactiveAccountAsInvalidCredentials(t *testing.T) {
	user := activeUser()
	user.Status = "SUSPENDED"
	service := newService(&fakeStore{user: user}, &fakeIssuer{}, &fakeAuditor{})

	if _, err := service.Login(context.Background(), "ana@melia.test", "correcta", session.Meta{}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("error = %v, want ErrInvalidCredentials: the status must not be revealed", err)
	}
}

func TestLoginSurvivesAnAuditFailure(t *testing.T) {
	store := &fakeStore{user: activeUser()}
	service := newService(store, &fakeIssuer{}, &fakeAuditor{err: errors.New("db down")})

	if _, err := service.Login(context.Background(), "ana@melia.test", "correcta", session.Meta{}); err != nil {
		t.Fatalf("Login failed because the audit write failed: %v", err)
	}
}
