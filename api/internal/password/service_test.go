package password

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/audit"
	"github.com/ivvanare/ecosystem_complete/api/internal/mailer"
	"github.com/ivvanare/ecosystem_complete/api/internal/security"
)

type fakeStore struct {
	userID      string
	userErr     error
	createErr   error
	created     []string
	row         *ResetRow
	rowErr      error
	attempts    int
	consumed    int
	newHash     string
	revoked     int
	commits     int
	rollbacks   int
	changeErr   error
	changedHash string
	password    string
	lastAt      *time.Time
	inWindow    int
}

func (f *fakeStore) ActiveUserID(context.Context, string) (string, error) {
	return f.userID, f.userErr
}

func (f *fakeStore) CreateResetToken(_ context.Context, _, _, codeHash string, _ time.Time) error {
	f.created = append(f.created, codeHash)
	return f.createErr
}

func (f *fakeStore) BeginReset(context.Context, string) (ResetTx, error) {
	if f.rowErr != nil {
		return nil, f.rowErr
	}
	return &fakeTx{store: f}, nil
}

func (f *fakeStore) LastRequestAt(context.Context, string) (*time.Time, error) {
	return f.lastAt, nil
}

func (f *fakeStore) CountRequestsSince(context.Context, string, time.Time) (int, error) {
	return f.inWindow, nil
}

func (f *fakeStore) PasswordHash(context.Context, string) (string, error) {
	return f.password, nil
}

func (f *fakeStore) ChangePassword(_ context.Context, _, hash string) error {
	if f.changeErr != nil {
		return f.changeErr
	}
	f.changedHash = hash
	f.revoked++
	return nil
}

type fakeTx struct{ store *fakeStore }

func (t *fakeTx) Token(context.Context) (ResetRow, error) {
	if t.store.row == nil {
		return ResetRow{}, ErrInvalidCode
	}
	return *t.store.row, nil
}

func (t *fakeTx) RecordFailedAttempt(context.Context) error {
	t.store.attempts++
	return nil
}

func (t *fakeTx) Consume(context.Context) error {
	t.store.consumed++
	return nil
}

func (t *fakeTx) SetPassword(_ context.Context, _, hash string) error {
	t.store.newHash = hash
	return nil
}

func (t *fakeTx) RevokeAllFamilies(context.Context, string) error {
	t.store.revoked++
	return nil
}

func (t *fakeTx) Commit(context.Context) error { t.store.commits++; return nil }

func (t *fakeTx) Rollback(context.Context) error { t.store.rollbacks++; return nil }

type fakePublisher struct{ messages [][]byte }

func (f *fakePublisher) Publish(_ context.Context, payload []byte) error {
	f.messages = append(f.messages, payload)
	return nil
}

type fakeAuditor struct{ events []audit.Event }

func (f *fakeAuditor) Record(_ context.Context, event audit.Event) error {
	f.events = append(f.events, event)
	return nil
}

var now = time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)

func newService(store Store, publisher Publisher, auditor audit.Recorder) *Service {
	service := New(store, publisher, auditor, slog.New(slog.DiscardHandler))
	service.Now = func() time.Time { return now }
	service.GenerateCode = func() (string, error) { return "042317", nil }
	return service
}

func TestRequestResetMailsACodeAndStoresOnlyItsHash(t *testing.T) {
	store := &fakeStore{userID: "user-1"}
	publisher := &fakePublisher{}
	auditor := &fakeAuditor{}
	service := newService(store, publisher, auditor)

	if err := service.RequestReset(context.Background(), " Ana@Melia.test "); err != nil {
		t.Fatalf("RequestReset returned error: %v", err)
	}

	if len(store.created) != 1 || store.created[0] != security.HashCode("042317") {
		t.Fatalf("stored hashes = %v, want the SHA-256 of the code", store.created)
	}

	if len(publisher.messages) != 1 {
		t.Fatalf("published %d messages", len(publisher.messages))
	}
	var message mailer.Message
	if err := json.Unmarshal(publisher.messages[0], &message); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	if message.To != "ana@melia.test" || !strings.Contains(message.Text, "042317") {
		t.Errorf("mail = %+v", message)
	}
	if len(auditor.events) != 1 || auditor.events[0].Name != audit.EventPasswordResetRequested {
		t.Errorf("audit = %v", auditor.events)
	}
}

func TestRequestResetForAnUnknownAddressIsSilent(t *testing.T) {
	store := &fakeStore{userErr: ErrNoUser}
	publisher := &fakePublisher{}
	service := newService(store, publisher, &fakeAuditor{})

	if err := service.RequestReset(context.Background(), "nadie@melia.test"); err != nil {
		t.Fatalf("RequestReset failed for an unknown address: %v", err)
	}
	if len(store.created) != 0 || len(publisher.messages) != 0 {
		t.Error("an unknown address produced a token or a mail")
	}
}

func TestRequestResetRetriesOnCodeCollision(t *testing.T) {
	store := &fakeStore{userID: "user-1", createErr: ErrCodeCollision}
	publisher := &fakePublisher{}
	service := newService(store, publisher, &fakeAuditor{})

	if err := service.RequestReset(context.Background(), "ana@melia.test"); err == nil {
		t.Fatal("RequestReset succeeded despite a permanent collision")
	}
	if len(store.created) != maxCodeTries {
		t.Errorf("tried %d times, want %d", len(store.created), maxCodeTries)
	}
	if len(publisher.messages) != 0 {
		t.Error("a mail was queued without a stored token")
	}
}

func liveResetRow() *ResetRow {
	return &ResetRow{
		TokenID:   "token-1",
		UserID:    "user-1",
		CodeHash:  security.HashCode("042317"),
		ExpiresAt: now.Add(5 * time.Minute),
	}
}

func TestResetSetsThePasswordAndKillsEverySession(t *testing.T) {
	store := &fakeStore{row: liveResetRow()}
	service := newService(store, &fakePublisher{}, &fakeAuditor{})

	if err := service.Reset(context.Background(), "ana@melia.test", "042317", "nueva-clave-larga-123"); err != nil {
		t.Fatalf("Reset returned error: %v", err)
	}

	if store.newHash == "" || store.newHash == "nueva-clave-larga-123" {
		t.Error("the new password was not hashed")
	}
	if store.consumed != 1 {
		t.Error("the code was not consumed")
	}
	if store.revoked != 1 {
		t.Error("sessions were not revoked: a stolen token would survive the reset")
	}
	if store.commits != 1 || store.rollbacks != 0 {
		t.Errorf("commits=%d rollbacks=%d", store.commits, store.rollbacks)
	}
}

func TestResetWithAWrongCodeCountsTheAttempt(t *testing.T) {
	store := &fakeStore{row: liveResetRow()}
	service := newService(store, &fakePublisher{}, &fakeAuditor{})

	if err := service.Reset(context.Background(), "ana@melia.test", "999999", "nueva-clave-larga-123"); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("error = %v, want ErrInvalidCode", err)
	}
	if store.attempts != 1 || store.commits != 1 {
		t.Errorf("attempts=%d commits=%d, want 1/1", store.attempts, store.commits)
	}
	if store.newHash != "" || store.revoked != 0 {
		t.Error("a wrong code changed the password or revoked sessions")
	}
}

func TestResetRejectsASpentCounterAndAnExpiredCode(t *testing.T) {
	spent := liveResetRow()
	spent.Attempts = MaxAttempts
	service := newService(&fakeStore{row: spent}, &fakePublisher{}, &fakeAuditor{})
	if err := service.Reset(context.Background(), "ana@melia.test", "042317", "nueva-clave-larga-123"); !errors.Is(err, ErrTooManyAttempts) {
		t.Errorf("spent counter: error = %v", err)
	}

	expired := liveResetRow()
	expired.ExpiresAt = now.Add(-time.Second)
	service = newService(&fakeStore{row: expired}, &fakePublisher{}, &fakeAuditor{})
	if err := service.Reset(context.Background(), "ana@melia.test", "042317", "nueva-clave-larga-123"); !errors.Is(err, ErrInvalidCode) {
		t.Errorf("expired: error = %v", err)
	}
}

func TestChangeVerifiesTheCurrentPasswordAndRevokesSessions(t *testing.T) {
	hash, err := security.HashPassword("la-vieja-clave-larga")
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}
	store := &fakeStore{password: hash}
	service := New(store, &fakePublisher{}, &fakeAuditor{}, slog.New(slog.DiscardHandler))

	if err := service.Change(context.Background(), "user-1", "otra-clave", "nueva-clave-larga-123"); !errors.Is(err, ErrInvalidCurrentPassword) {
		t.Fatalf("error = %v, want ErrInvalidCurrentPassword", err)
	}
	if store.changedHash != "" {
		t.Error("the password changed without the current one")
	}

	if err := service.Change(context.Background(), "user-1", "la-vieja-clave-larga", "nueva-clave-larga-123"); err != nil {
		t.Fatalf("Change returned error: %v", err)
	}
	if store.changedHash == "" || store.revoked != 1 {
		t.Error("the password was not changed or sessions were not revoked")
	}
}

func TestChangeRejectsAnAccountWithoutAPassword(t *testing.T) {
	service := New(&fakeStore{password: ""}, &fakePublisher{}, &fakeAuditor{}, slog.New(slog.DiscardHandler))

	if err := service.Change(context.Background(), "user-1", "x", "nueva-clave-larga-123"); !errors.Is(err, ErrInvalidCurrentPassword) {
		t.Fatalf("error = %v, want ErrInvalidCurrentPassword for a federated-only account", err)
	}
}

func TestRequestResetIsSilentWhenTheQuotaIsExhausted(t *testing.T) {
	recent := now.Add(-5 * time.Second)
	store := &fakeStore{userID: "user-1", lastAt: &recent}
	publisher := &fakePublisher{}
	auditor := &fakeAuditor{}
	service := newService(store, publisher, auditor)

	// Silence, not 429: a 429 here would reveal that the account exists.
	if err := service.RequestReset(context.Background(), "ana@melia.test"); err != nil {
		t.Fatalf("RequestReset returned error: %v", err)
	}
	if len(store.created) != 0 || len(publisher.messages) != 0 {
		t.Error("a throttled request still created a token or queued a mail")
	}
	if len(auditor.events) != 1 {
		t.Fatalf("audit = %v, want one event", auditor.events)
	}
	if reason, _ := auditor.events[0].Metadata["reason"].(string); reason != "quota" {
		t.Errorf("reason = %v, want quota", auditor.events[0].Metadata["reason"])
	}
}

func TestRequestResetIsThrottledByTheWindowLimit(t *testing.T) {
	store := &fakeStore{userID: "user-1", inWindow: 5}
	publisher := &fakePublisher{}
	service := newService(store, publisher, &fakeAuditor{})

	if err := service.RequestReset(context.Background(), "ana@melia.test"); err != nil {
		t.Fatalf("RequestReset returned error: %v", err)
	}
	if len(publisher.messages) != 0 {
		t.Error("the daily limit did not stop the mail")
	}
}
