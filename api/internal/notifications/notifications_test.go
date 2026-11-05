package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/ivvanare/ecosystem_complete/api/internal/mailer"
)

type fakeStore struct {
	disabled   []string
	email      string
	name       string
	setKind    string
	setValue   bool
	contactErr error
}

func (f *fakeStore) Disabled(context.Context, string) ([]string, error) { return f.disabled, nil }
func (f *fakeStore) SetDisabled(_ context.Context, _, kind string, disabled bool) error {
	f.setKind, f.setValue = kind, disabled
	return nil
}
func (f *fakeStore) Contact(context.Context, string) (string, string, error) {
	return f.email, f.name, f.contactErr
}

type fakePublisher struct {
	sent []mailer.Message
	err  error
}

func (f *fakePublisher) Publish(_ context.Context, payload []byte) error {
	if f.err != nil {
		return f.err
	}
	var message mailer.Message
	if err := json.Unmarshal(payload, &message); err != nil {
		return err
	}
	f.sent = append(f.sent, message)
	return nil
}

func service(store Store, publisher Publisher) *Service {
	return NewService(store, publisher, slog.New(slog.DiscardHandler))
}

// S13: each kind is individually disableable, and the announcement never fails the act.

func TestInvitationNotificationIsDeliveredByEmail(t *testing.T) {
	store := &fakeStore{email: "invitee@melia.test", name: "Ana"}
	publisher := &fakePublisher{}
	service(store, publisher).InvitationReceived(context.Background(), "u1", "Asado del sábado")

	if len(publisher.sent) != 1 {
		t.Fatalf("queued %d messages, want 1", len(publisher.sent))
	}
	message := publisher.sent[0]
	if message.To != "invitee@melia.test" {
		t.Fatalf("To = %q", message.To)
	}
	if message.Subject == "" || message.Text == "" {
		t.Fatalf("message is empty: %+v", message)
	}
}

func TestADisabledKindIsNotDelivered(t *testing.T) {
	store := &fakeStore{disabled: []string{KindInvitationReceived}, email: "invitee@melia.test"}
	publisher := &fakePublisher{}
	service(store, publisher).InvitationReceived(context.Background(), "u1", "Asado")

	if len(publisher.sent) != 0 {
		t.Fatalf("a disabled kind was delivered: %+v", publisher.sent)
	}
}

func TestDeliveryFailureNeverPropagates(t *testing.T) {
	// A courtesy must not be able to fail the thing it announces: the invitation already
	// exists by the time this runs.
	store := &fakeStore{email: "invitee@melia.test"}
	service(store, &fakePublisher{err: errors.New("broker down")}).
		InvitationReceived(context.Background(), "u1", "Asado")

	noContact := &fakeStore{contactErr: errors.New("no user")}
	service(noContact, &fakePublisher{}).InvitationReceived(context.Background(), "u1", "Asado")
}

func TestUnknownKindIsRejected(t *testing.T) {
	store := &fakeStore{}
	if err := service(store, &fakePublisher{}).SetPreference(context.Background(), "u1", "made.up", true); err == nil {
		t.Fatal("an unknown kind was accepted")
	}
	if store.setKind != "" {
		t.Fatal("the store was reached with an unknown kind")
	}
}

func TestPreferencesAreResolvedAgainstTheCatalogue(t *testing.T) {
	store := &fakeStore{}
	service := service(store, &fakePublisher{})

	if err := service.SetPreference(context.Background(), "u1", KindInvitationReceived, true); err != nil {
		t.Fatalf("SetPreference: %v", err)
	}
	if store.setKind != KindInvitationReceived || !store.setValue {
		t.Fatalf("stored %q=%v", store.setKind, store.setValue)
	}
	if !IsKnownKind(KindInvitationReceived) || IsKnownKind("nope") {
		t.Fatal("the catalogue check is wrong")
	}
}
