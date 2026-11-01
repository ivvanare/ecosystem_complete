// Package invitation emails the invitation an administrator sends to a future panel user.
//
// It carries no state on purpose: the person still registers, proves the address with the
// code we send them, and the administrator grants the role afterwards. Nothing is created
// before verification — that is the model's rule (diagram_base.txt:74).
package invitation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ivvanare/ecosystem_complete/api/internal/mailer"
)

// Publisher queues a payload for the mail worker.
type Publisher interface {
	Publish(ctx context.Context, payload []byte) error
}

// Service turns an invitation into one queued message.
type Service struct {
	Publisher Publisher
	// Origin is the address the message points at, normally the panel's URL.
	Origin string
	Logger *slog.Logger
}

// New returns a sender. An empty origin degrades the text instead of failing: the mail
// still tells the person to sign in, which is the part that matters.
func New(publisher Publisher, origin string, logger *slog.Logger) *Service {
	return &Service{
		Publisher: publisher,
		Origin:    strings.TrimRight(strings.TrimSpace(origin), "/"),
		Logger:    logger,
	}
}

// Invite queues the invitation. It never fails because the address already has an
// account: the text covers both cases, so the endpoint cannot be used to probe whether
// an email is registered.
func (s *Service) Invite(ctx context.Context, email, name string) error {
	if s.Publisher == nil {
		return fmt.Errorf("invitation: no publisher configured")
	}

	greeting := "Hello"
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		greeting = "Hello " + trimmed
	}
	where := s.Origin + "/registro"
	if s.Origin == "" {
		where = "the melZone registration page"
	}

	text := fmt.Sprintf(`%s,

An administrator invited you to the melZone panel.

If you do not have an account with this address yet, register with it here: %s

We will email you a six-digit code to confirm the address; the account is created only
after you enter it.

If you already have an account, sign in at the same address. The administrator will grant
you the access you need; invitations do not grant anything by themselves.

If you were not expecting this message, you can ignore it.
`, greeting, where)

	payload, err := json.Marshal(mailer.Message{
		To:      email,
		Subject: "You have been invited to the melZone panel",
		Text:    text,
	})
	if err != nil {
		return fmt.Errorf("invitation: encoding mail message: %w", err)
	}
	if err := s.Publisher.Publish(ctx, payload); err != nil {
		return fmt.Errorf("invitation: queueing mail: %w", err)
	}

	if s.Logger != nil {
		// The address is not logged: the worker already knows the recipient, and logs
		// outlive the account.
		s.Logger.Info("invitation queued")
	}
	return nil
}
