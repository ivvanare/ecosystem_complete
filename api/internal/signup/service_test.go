package signup

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/mailer"
	"github.com/ivvanare/ecosystem_complete/api/internal/security"
)

type fakeStore struct {
	exists   bool
	err      error
	upserts  int
	upserted Pending
}

func (f *fakeStore) ActiveUserExists(context.Context, string) (bool, error) {
	return f.exists, f.err
}

func (f *fakeStore) UpsertPending(_ context.Context, pending Pending) error {
	f.upserts++
	f.upserted = pending
	return f.err
}

type fakePublisher struct {
	messages [][]byte
	err      error
}

func (f *fakePublisher) Publish(_ context.Context, payload []byte) error {
	f.messages = append(f.messages, payload)
	return f.err
}

func (f *fakePublisher) last(t *testing.T) mailer.Message {
	t.Helper()
	if len(f.messages) == 0 {
		t.Fatal("nothing was published")
	}
	var message mailer.Message
	if err := json.Unmarshal(f.messages[len(f.messages)-1], &message); err != nil {
		t.Fatalf("published payload is not JSON: %v", err)
	}
	return message
}

func newTestService(store Store, publisher Publisher) *Service {
	service := New(store, publisher, slog.New(slog.DiscardHandler))
	service.Now = func() time.Time { return time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC) }
	service.GenerateCode = func() (string, error) { return "042317", nil }
	return service
}

func TestRequestStoresThePendingRegistrationAndMailsTheCode(t *testing.T) {
	store := &fakeStore{}
	publisher := &fakePublisher{}
	service := newTestService(store, publisher)

	if err := service.Request(context.Background(), "ana@melia.test", "Ana", "una-clave-larga-123"); err != nil {
		t.Fatalf("Request returned error: %v", err)
	}

	if store.upserts != 1 {
		t.Fatalf("upserts = %d, want 1", store.upserts)
	}
	pending := store.upserted
	if pending.Email != "ana@melia.test" || pending.Name != "Ana" {
		t.Errorf("pending = %+v", pending)
	}
	if pending.PasswordHash == "una-clave-larga-123" || strings.Contains(pending.PasswordHash, "una-clave") {
		t.Error("the clear password reached the store")
	}
	if pending.CodeHash == "042317" || strings.Contains(pending.CodeHash, "042317") {
		t.Error("the clear code reached the store")
	}
	if pending.CodeHash != security.HashCode("042317") {
		t.Error("the stored hash is not the SHA-256 of the issued code")
	}
	if want := service.Now().Add(CodeLifetime); !pending.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want %v", pending.ExpiresAt, want)
	}

	message := publisher.last(t)
	if message.To != "ana@melia.test" {
		t.Errorf("mail sent to %q", message.To)
	}
	if !strings.Contains(message.Text, "042317") {
		t.Errorf("the mail does not carry the code: %q", message.Text)
	}
}

func TestRequestForAnExistingAccountSendsNoCode(t *testing.T) {
	store := &fakeStore{exists: true}
	publisher := &fakePublisher{}
	service := newTestService(store, publisher)

	if err := service.Request(context.Background(), "ana@melia.test", "Ana", "una-clave-larga-123"); err != nil {
		t.Fatalf("Request returned error: %v", err)
	}

	if store.upserts != 0 {
		t.Errorf("upserts = %d, want 0: an existing account must not get a pending registration", store.upserts)
	}
	message := publisher.last(t)
	if strings.Contains(message.Text, "042317") {
		t.Error("a code was sent to an existing account")
	}
	if !strings.Contains(strings.ToLower(message.Subject), "cuenta") {
		t.Errorf("unexpected subject for an existing account: %q", message.Subject)
	}
}

func TestRequestNormalizesTheEmail(t *testing.T) {
	store := &fakeStore{}
	service := newTestService(store, &fakePublisher{})

	if err := service.Request(context.Background(), "  ANA@Melia.Test  ", "Ana", "una-clave-larga-123"); err != nil {
		t.Fatalf("Request returned error: %v", err)
	}
	if store.upserted.Email != "ana@melia.test" {
		t.Errorf("stored email = %q, want normalized", store.upserted.Email)
	}
}

func TestRequestFailsWhenTheStoreFails(t *testing.T) {
	publisher := &fakePublisher{}
	service := newTestService(&fakeStore{err: errors.New("db down")}, publisher)

	if err := service.Request(context.Background(), "ana@melia.test", "Ana", "una-clave-larga-123"); err == nil {
		t.Fatal("Request succeeded, want error")
	}
	if len(publisher.messages) != 0 {
		t.Error("a mail was queued despite the store failing")
	}
}

func TestRequestFailsWhenTheQueueFails(t *testing.T) {
	store := &fakeStore{}
	service := newTestService(store, &fakePublisher{err: errors.New("broker down")})

	err := service.Request(context.Background(), "ana@melia.test", "Ana", "una-clave-larga-123")
	if err == nil {
		t.Fatal("Request succeeded, want error: a code nobody received must not look successful")
	}
}
