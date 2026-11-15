// Package push turns "this person should know something" into deliveries, and nothing else.
//
// It is the transport half of a notification: it reads the devices a person registered, asks
// whether they still want this kind, and hands the message to a Sender. What the message says,
// and when it is worth sending, belongs to the feature that calls it.
//
// Every failure here is a log line, never an error: a notification is a courtesy, and a
// courtesy must not be able to fail the thing it announces. The same rule the mail path already
// follows.
package push

import (
	"context"
	"log/slog"
)

// Message is what a device shows.
type Message struct {
	// Title and Body are the human text. Keep them short: the OS truncates, and a
	// notification nobody can read is a notification nobody taps.
	Title string
	Body  string
	// Data travels with the message for the client to route on, e.g. {"kind": "..."}.
	Data map[string]string
}

// Sender delivers to a provider (FCM, APNs).
//
// It takes the whole batch of a user's tokens because one person has several devices and the
// provider accepts a list: sending them one call each would be N round trips for one event.
// A sender that cannot reach one device must still deliver to the others, so a partial failure
// is the sender's business to report, not the caller's to untangle.
type Sender interface {
	Send(ctx context.Context, tokens []string, message Message) error
}

// Tokens is the device registry read.
type Tokens interface {
	Tokens(ctx context.Context, userID string) ([]string, error)
}

// Preferences answers whether this person still wants a kind of notification.
type Preferences interface {
	Disabled(ctx context.Context, userID string) ([]string, error)
}

// Service dispatches one notification to one person.
type Service struct {
	tokens Tokens
	prefs  Preferences
	sender Sender
	logger *slog.Logger
}

// New wires the dispatcher. A nil sender is a real state: the provider is not configured yet,
// and the right answer then is to do nothing quietly, not to accept and fail.
func New(tokens Tokens, prefs Preferences, sender Sender, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{tokens: tokens, prefs: prefs, sender: sender, logger: logger}
}

// Send delivers to every live device of the user that still wants this kind.
//
// It returns nothing on purpose. There is no caller that could do anything useful with a
// failure: the invitation already exists, and failing the request would undo a real action
// because a best-effort courtesy did not arrive.
func (s *Service) Send(ctx context.Context, userID, kind string, message Message) {
	if s == nil || s.sender == nil {
		// No provider configured: the token is stored and unused, which is harmless.
		return
	}

	disabled, err := s.prefs.Disabled(ctx, userID)
	if err != nil {
		s.logger.Warn("push skipped: preferences unavailable", slog.String("error", err.Error()))
		return
	}
	for _, off := range disabled {
		if off == kind {
			return
		}
	}

	tokens, err := s.tokens.Tokens(ctx, userID)
	if err != nil {
		s.logger.Warn("push skipped: devices unavailable", slog.String("error", err.Error()))
		return
	}
	if len(tokens) == 0 {
		// Not a problem: the person may not have opened the app on a phone yet.
		s.logger.Debug("push skipped: no registered device", slog.String("kind", kind))
		return
	}

	if err := s.sender.Send(ctx, tokens, message); err != nil {
		s.logger.Warn("push failed",
			slog.String("kind", kind),
			slog.Int("devices", len(tokens)),
			slog.String("error", err.Error()))
		return
	}
	s.logger.Info("push sent", slog.String("kind", kind), slog.Int("devices", len(tokens)))
}
