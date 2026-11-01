package invitation

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ivvanare/ecosystem_complete/api/internal/mailer"
)

type fakePublisher struct {
	payload []byte
	err     error
}

func (f *fakePublisher) Publish(_ context.Context, payload []byte) error {
	f.payload = payload
	return f.err
}

func TestInvitePointsAtTheRegistrationPage(t *testing.T) {
	publisher := &fakePublisher{}
	service := New(publisher, "https://panel.test/", nil)

	if err := service.Invite(context.Background(), "ana@melia.test", "Ana"); err != nil {
		t.Fatalf("Invite: %v", err)
	}

	var message mailer.Message
	if err := json.Unmarshal(publisher.payload, &message); err != nil {
		t.Fatalf("decoding the queued message: %v", err)
	}
	if message.To != "ana@melia.test" {
		t.Errorf("to = %q", message.To)
	}
	// The link has to be the page that can actually complete a registration; before this
	// the message pointed at the panel root, which offers no signup at all.
	if !strings.Contains(message.Text, "https://panel.test/registro") {
		t.Errorf("text does not point at the registration page: %s", message.Text)
	}
	if !strings.Contains(message.Text, "six-digit code") {
		t.Errorf("text does not explain the code: %s", message.Text)
	}
}
