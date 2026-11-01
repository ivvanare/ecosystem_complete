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

	greeting := "Hola"
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		greeting = "Hola " + trimmed
	}
	where := s.Origin + "/registro"
	if s.Origin == "" {
		where = "la página de registro de melZone"
	}

	text := fmt.Sprintf(`%s,

Un administrador te invitó al panel de melZone.

Si todavía no tenés cuenta con esta dirección, registrate acá: %s

Te vamos a mandar un código de seis dígitos para confirmar la dirección; la cuenta se crea
recién cuando lo ingreses.

Si ya tenés cuenta, iniciá sesión con la misma dirección. El administrador te va a dar el
acceso que necesites: la invitación por sí sola no otorga nada.

Si no esperabas este mensaje, podés ignorarlo.
`, greeting, where)

	payload, err := json.Marshal(mailer.Message{
		To:      email,
		Subject: "Te invitaron al panel de melZone",
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
