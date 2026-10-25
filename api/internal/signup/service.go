// Package signup implements the first half of registration: proving ownership of the
// email before the user exists.
//
// The pending registration lives in seg.tb_signup_request, never in tb_user, so nobody can
// squat an address they cannot read.
package signup

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/mailer"
	"github.com/ivvanare/ecosystem_complete/api/internal/security"
)

// CodeLifetime is how long a signup code stays valid. The model caps it at 10 minutes.
const CodeLifetime = 10 * time.Minute

// Pending is one row of seg.tb_signup_request.
type Pending struct {
	Email        string
	Name         string
	PasswordHash string
	CodeHash     string
	ExpiresAt    time.Time
}

// Store persists the pending registration.
type Store interface {
	// ActiveUserExists reports whether a non-deleted user already owns the email.
	ActiveUserExists(ctx context.Context, email string) (bool, error)
	// UpsertPending writes or replaces the pending registration for the email.
	UpsertPending(ctx context.Context, pending Pending) error
}

// Publisher delivers the mail message to the queue.
type Publisher interface {
	Publish(ctx context.Context, payload []byte) error
}

// Service requests a signup code.
type Service struct {
	Store     Store
	Publisher Publisher
	Logger    *slog.Logger

	// Injectable for tests.
	Now          func() time.Time
	GenerateCode func() (string, error)
	HashPassword func(string) (string, error)
}

// New returns a Service with the real clock, entropy and hashing wired in.
func New(store Store, publisher Publisher, logger *slog.Logger) *Service {
	return &Service{
		Store:        store,
		Publisher:    publisher,
		Logger:       logger,
		Now:          time.Now,
		GenerateCode: security.GenerateCode,
		HashPassword: security.HashPassword,
	}
}

// Request starts a registration.
//
// It always succeeds from the caller's point of view when the address is already
// registered: the owner gets a different email and the response is identical, so the
// endpoint cannot be used to enumerate accounts. Infrastructure failures are returned,
// because silently pretending a code was sent leaves the user waiting forever.
func (s *Service) Request(ctx context.Context, email, name, password string) error {
	email = normalizeEmail(email)

	exists, err := s.Store.ActiveUserExists(ctx, email)
	if err != nil {
		return fmt.Errorf("signup: checking existing user: %w", err)
	}
	if exists {
		if err := s.publish(ctx, mailer.Message{
			To:      email,
			Subject: "Ya tenés una cuenta en melia",
			Text: "Alguien intentó registrarse con este correo, pero ya tiene una cuenta.\n" +
				"Si fuiste vos, iniciá sesión o recuperá tu contraseña. Si no fuiste vos, ignorá este mensaje.",
		}); err != nil {
			return err
		}
		s.Logger.Info("signup attempted for an existing account", slog.String("email", redact(email)))
		return nil
	}

	passwordHash, err := s.HashPassword(password)
	if err != nil {
		return fmt.Errorf("signup: hashing password: %w", err)
	}

	code, err := s.GenerateCode()
	if err != nil {
		return fmt.Errorf("signup: generating code: %w", err)
	}

	pending := Pending{
		Email:        email,
		Name:         strings.TrimSpace(name),
		PasswordHash: passwordHash,
		CodeHash:     security.HashCode(code),
		ExpiresAt:    s.Now().Add(CodeLifetime),
	}

	if err := s.Store.UpsertPending(ctx, pending); err != nil {
		return fmt.Errorf("signup: storing pending registration: %w", err)
	}

	if err := s.publish(ctx, mailer.Message{
		To:      email,
		Subject: "Tu código de registro",
		Text: fmt.Sprintf("Tu código para terminar el registro es %s.\n"+
			"Vence en 10 minutos y sirve una sola vez.", code),
	}); err != nil {
		return err
	}

	s.Logger.Info("signup code requested", slog.String("email", redact(email)))
	return nil
}

func (s *Service) publish(ctx context.Context, message mailer.Message) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("signup: encoding mail message: %w", err)
	}
	if err := s.Publisher.Publish(ctx, payload); err != nil {
		return fmt.Errorf("signup: queueing mail: %w", err)
	}
	return nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// redact keeps logs useful without writing the full address.
func redact(email string) string {
	at := strings.LastIndex(email, "@")
	if at <= 1 {
		return "***"
	}
	return email[:1] + "***" + email[at:]
}
