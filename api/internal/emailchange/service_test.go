package emailchange

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
	current   string
	taken     bool
	password  string
	created   []string
	createErr error
	row       *ChangeRow
	attempts  int
	consumed  int
	newEmail  string
	revoked   int
	commits   int
	rollbacks int
	lastAt    *time.Time
	inWindow  int
}

func (f *fakeStore) PasswordHash(context.Context, string) (string, error) { return f.password, nil }

func (f *fakeStore) CurrentEmail(context.Context, string) (string, error) { return f.current, nil }

func (f *fakeStore) AddressTaken(context.Context, string) (bool, error) { return f.taken, nil }

func (f *fakeStore) CreateChangeToken(_ context.Context, _, target, codeHash string, _ time.Time) error {
	f.created = append(f.created, target+"|"+codeHash)
	return f.createErr
}

func (f *fakeStore) LastRequestAt(context.Context, string) (*time.Time, error) { return f.lastAt, nil }

func (f *fakeStore) CountRequestsSince(context.Context, string, time.Time) (int, error) {
	return f.inWindow, nil
}

func (f *fakeStore) BeginVerify(context.Context, string) (VerifyTx, error) {
	return &fakeTx{store: f}, nil
}

type fakeTx struct{ store *fakeStore }

func (t *fakeTx) Token(context.Context) (ChangeRow, error) {
	if t.store.row == nil {
		return ChangeRow{}, ErrInvalidCode
	}
	return *t.store.row, nil
}

func (t *fakeTx) RecordFailedAttempt(context.Context) error { t.store.attempts++; return nil }
func (t *fakeTx) Consume(context.Context) error             { t.store.consumed++; return nil }
func (t *fakeTx) SetEmail(_ context.Context, _, email string) error {
	t.store.newEmail = email
	return nil
}
func (t *fakeTx) RevokeAllFamilies(context.Context, string) error { t.store.revoked++; return nil }
func (t *fakeTx) Commit(context.Context) error                    { t.store.commits++; return nil }
func (t *fakeTx) Rollback(context.Context) error                  { t.store.rollbacks++; return nil }

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
	service.VerifyPassword = func(encoded, plain string) (bool, error) {
		return encoded == "argon2id$hash" && plain == "correcta", nil
	}
	return service
}

func activeStore() *fakeStore {
	return &fakeStore{current: "vieja@melia.test", password: "argon2id$hash"}
}

func TestRequestMailsTheNewAddressAndRequiresThePassword(t *testing.T) {
	store := activeStore()
	publisher := &fakePublisher{}
	service := newService(store, publisher, &fakeAuditor{})

	if err := service.Request(context.Background(), "user-1", " Nueva@Melia.test ", "correcta"); err != nil {
		t.Fatalf("Request returned error: %v", err)
	}

	if len(store.created) != 1 {
		t.Fatalf("created %v, want one token", store.created)
	}
	if !strings.HasPrefix(store.created[0], "nueva@melia.test|") {
		t.Errorf("token target = %q, want the normalized new address", store.created[0])
	}
	if strings.Contains(store.created[0], "042317") {
		t.Error("the clear code reached the store")
	}

	var message mailer.Message
	if err := json.Unmarshal(publisher.messages[0], &message); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	if message.To != "nueva@melia.test" || !strings.Contains(message.Text, "042317") {
		t.Errorf("mail = %+v", message)
	}
}

func TestRequestRejectsAWrongPassword(t *testing.T) {
	service := newService(activeStore(), &fakePublisher{}, &fakeAuditor{})

	if err := service.Request(context.Background(), "user-1", "nueva@melia.test", "mala"); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("error = %v, want ErrInvalidPassword", err)
	}
}

func TestRequestRejectsATakenAddressAndTheCurrentOne(t *testing.T) {
	taken := activeStore()
	taken.taken = true
	if err := newService(taken, &fakePublisher{}, &fakeAuditor{}).
		Request(context.Background(), "user-1", "otra@melia.test", "correcta"); !errors.Is(err, ErrEmailTaken) {
		t.Errorf("taken address: error = %v", err)
	}

	same := activeStore()
	if err := newService(same, &fakePublisher{}, &fakeAuditor{}).
		Request(context.Background(), "user-1", "VIEJA@melia.test", "correcta"); !errors.Is(err, ErrSameEmail) {
		t.Errorf("same address: error = %v", err)
	}
}

func TestRequestIsThrottled(t *testing.T) {
	store := activeStore()
	recent := now.Add(-5 * time.Second)
	store.lastAt = &recent
	publisher := &fakePublisher{}
	service := newService(store, publisher, &fakeAuditor{})

	err := service.Request(context.Background(), "user-1", "nueva@melia.test", "correcta")

	var quota *security.TooManyRequestsError
	if !errors.As(err, &quota) {
		t.Fatalf("error = %v, want *TooManyRequestsError", err)
	}
	if quota.RetryAfter != 55*time.Second {
		t.Errorf("RetryAfter = %v, want the cooldown remainder", quota.RetryAfter)
	}
	if len(publisher.messages) != 0 || len(store.created) != 0 {
		t.Error("a throttled request still mailed or stored a token")
	}

	store.lastAt = nil
	store.inWindow = security.DefaultLimit
	if err := service.Request(context.Background(), "user-1", "nueva@melia.test", "correcta"); !errors.As(err, &quota) {
		t.Errorf("daily limit: error = %v", err)
	}
}

func liveRow() *ChangeRow {
	return &ChangeRow{
		TokenID:   "token-1",
		UserID:    "user-1",
		CodeHash:  security.HashCode("042317"),
		Target:    "nueva@melia.test",
		ExpiresAt: now.Add(5 * time.Minute),
	}
}

func TestVerifyAppliesTheChangeAndKillsEverySession(t *testing.T) {
	store := activeStore()
	store.row = liveRow()
	service := newService(store, &fakePublisher{}, &fakeAuditor{})

	if err := service.Verify(context.Background(), "user-1", "042317"); err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
	if store.newEmail != "nueva@melia.test" {
		t.Errorf("email set to %q", store.newEmail)
	}
	if store.consumed != 1 || store.revoked != 1 {
		t.Errorf("consumed=%d revoked=%d, want 1/1", store.consumed, store.revoked)
	}
	if store.commits != 1 || store.rollbacks != 0 {
		t.Errorf("commits=%d rollbacks=%d", store.commits, store.rollbacks)
	}
}

func TestVerifyWithAWrongCodeCountsTheAttempt(t *testing.T) {
	store := activeStore()
	store.row = liveRow()
	service := newService(store, &fakePublisher{}, &fakeAuditor{})

	if err := service.Verify(context.Background(), "user-1", "999999"); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("error = %v", err)
	}
	if store.attempts != 1 || store.commits != 1 {
		t.Errorf("attempts=%d commits=%d", store.attempts, store.commits)
	}
	if store.newEmail != "" || store.revoked != 0 {
		t.Error("a wrong code changed the email")
	}
}

func TestVerifyRejectsAnAddressClaimedMeanwhile(t *testing.T) {
	store := activeStore()
	store.row = liveRow()
	store.taken = true

	if err := newService(store, &fakePublisher{}, &fakeAuditor{}).
		Verify(context.Background(), "user-1", "042317"); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("error = %v, want ErrEmailTaken", err)
	}
	if store.newEmail != "" {
		t.Error("a taken address was applied")
	}
}

func TestVerifyRejectsExpiredAndSpentCodes(t *testing.T) {
	expired := activeStore()
	row := liveRow()
	row.ExpiresAt = now.Add(-time.Second)
	expired.row = row
	if err := newService(expired, &fakePublisher{}, &fakeAuditor{}).
		Verify(context.Background(), "user-1", "042317"); !errors.Is(err, ErrInvalidCode) {
		t.Errorf("expired: error = %v", err)
	}

	spent := activeStore()
	row = liveRow()
	row.Attempts = MaxAttempts
	spent.row = row
	if err := newService(spent, &fakePublisher{}, &fakeAuditor{}).
		Verify(context.Background(), "user-1", "042317"); !errors.Is(err, ErrTooManyAttempts) {
		t.Errorf("spent: error = %v", err)
	}
}
