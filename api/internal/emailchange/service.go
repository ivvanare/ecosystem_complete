// Package emailchange moves an account to a new address after proving ownership of it.
//
// The code goes to the NEW address, and the current password is required: a stolen access
// token alone must not be able to move an account. A successful change revokes every refresh
// family, like every other credential change.
package emailchange

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/audit"
	"github.com/ivvanare/ecosystem_complete/api/internal/mailer"
	"github.com/ivvanare/ecosystem_complete/api/internal/security"
)

// Errors mapped onto the contract.
var (
	ErrInvalidCode     = errors.New("emailchange: code is invalid or expired")
	ErrTooManyAttempts = errors.New("emailchange: too many attempts")
	ErrEmailTaken      = errors.New("emailchange: the address already belongs to an account")
	ErrInvalidPassword = errors.New("emailchange: password does not match")
	ErrSameEmail       = errors.New("emailchange: the address is already the account's")
	ErrCodeCollision   = errors.New("emailchange: code collision")
)

// Constants from the model: short life, few attempts.
const (
	CodeLifetime = 10 * time.Minute
	MaxAttempts  = 5
	maxCodeTries = 3
)

// ChangeRow is the locked token row.
type ChangeRow struct {
	TokenID   string
	UserID    string
	CodeHash  string
	Target    string
	Attempts  int
	ExpiresAt time.Time
	UsedAt    *time.Time
}

// VerifyTx is one transaction over the change token.
type VerifyTx interface {
	Token(ctx context.Context) (ChangeRow, error)
	RecordFailedAttempt(ctx context.Context) error
	Consume(ctx context.Context) error
	SetEmail(ctx context.Context, userID, newEmail string) error
	RevokeAllFamilies(ctx context.Context, userID string) error
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// Store persists the change state.
type Store interface {
	// PasswordHash returns the stored hash, or "" for a federated-only account.
	PasswordHash(ctx context.Context, userID string) (string, error)
	// CurrentEmail returns the account's address.
	CurrentEmail(ctx context.Context, userID string) (string, error)
	// AddressTaken reports whether another active account owns the address.
	AddressTaken(ctx context.Context, email string) (bool, error)
	// CreateChangeToken invalidates previous pending changes and stores the new one.
	CreateChangeToken(ctx context.Context, userID, target, codeHash string, expiresAt time.Time) error
	// LastRequestAt returns when the user last asked for a change, or nil.
	LastRequestAt(ctx context.Context, userID string) (*time.Time, error)
	// CountRequestsSince counts the requests made after the given instant.
	CountRequestsSince(ctx context.Context, userID string, since time.Time) (int, error)
	// BeginVerify opens the transaction for the newest unused token.
	BeginVerify(ctx context.Context, userID string) (VerifyTx, error)
}

// Publisher queues the mail.
type Publisher interface {
	Publish(ctx context.Context, payload []byte) error
}

// Service handles the email change flow.
type Service struct {
	Store     Store
	Publisher Publisher
	Auditor   audit.Recorder
	Logger    *slog.Logger

	Now            func() time.Time
	GenerateCode   func() (string, error)
	VerifyPassword func(encoded, plain string) (bool, error)

	// Quota bounds how often a code can be requested. Without it, every reissue resets the
	// per-token attempt counter, which turns "five tries" into unlimited guesses.
	Quota security.Quota
}

// New returns a Service.
func New(store Store, publisher Publisher, auditor audit.Recorder, logger *slog.Logger) *Service {
	return &Service{
		Store:          store,
		Publisher:      publisher,
		Auditor:        auditor,
		Logger:         logger,
		Now:            time.Now,
		GenerateCode:   security.GenerateCode,
		VerifyPassword: security.VerifyPassword,
		Quota:          security.Quota{},
	}
}

// Request mails a code to the new address.
func (s *Service) Request(ctx context.Context, userID, newEmail, currentPassword string) error {
	newEmail = normalize(newEmail)

	current, err := s.Store.CurrentEmail(ctx, userID)
	if err != nil {
		return fmt.Errorf("emailchange: reading current email: %w", err)
	}
	if normalize(current) == newEmail {
		return ErrSameEmail
	}

	taken, err := s.Store.AddressTaken(ctx, newEmail)
	if err != nil {
		return fmt.Errorf("emailchange: checking address: %w", err)
	}
	if taken {
		return ErrEmailTaken
	}

	stored, err := s.Store.PasswordHash(ctx, userID)
	if err != nil {
		return fmt.Errorf("emailchange: reading password hash: %w", err)
	}
	if stored == "" {
		return ErrInvalidPassword
	}
	ok, err := s.VerifyPassword(stored, currentPassword)
	if err != nil || !ok {
		s.record(ctx, audit.Event{Name: audit.EventEmailChangeFailed, UserID: userID,
			Metadata: map[string]any{"reason": "wrong_password"}})
		return ErrInvalidPassword
	}

	now := s.now()
	lastAt, err := s.Store.LastRequestAt(ctx, userID)
	if err != nil {
		return fmt.Errorf("emailchange: reading last request: %w", err)
	}
	inWindow, err := s.Store.CountRequestsSince(ctx, userID, now.Add(-s.window()))
	if err != nil {
		return fmt.Errorf("emailchange: counting requests: %w", err)
	}
	if err := s.Quota.Check(now, lastAt, inWindow); err != nil {
		s.record(ctx, audit.Event{Name: audit.EventEmailChangeFailed, UserID: userID,
			Metadata: map[string]any{"reason": "quota"}})
		return err
	}

	var code string
	for attempt := 0; attempt < maxCodeTries; attempt++ {
		code, err = s.GenerateCode()
		if err != nil {
			return fmt.Errorf("emailchange: generating code: %w", err)
		}
		err = s.Store.CreateChangeToken(ctx, userID, newEmail, security.HashCode(code), now.Add(CodeLifetime))
		if err == nil {
			break
		}
		if !errors.Is(err, ErrCodeCollision) {
			return fmt.Errorf("emailchange: storing token: %w", err)
		}
	}
	if err != nil {
		return fmt.Errorf("emailchange: no free code after %d tries: %w", maxCodeTries, err)
	}

	if err := s.publish(ctx, mailer.Message{
		To:      newEmail,
		Subject: "Confirmá tu nuevo email",
		Text: fmt.Sprintf("Tu código para confirmar esta dirección es %s.\n"+
			"Vence en 10 minutos y sirve una sola vez. Si no lo pediste, ignorá este mensaje.", code),
	}); err != nil {
		return err
	}

	s.record(ctx, audit.Event{Name: audit.EventEmailChangeRequested, UserID: userID, Email: newEmail})
	return nil
}

// Verify applies the change and ends every session.
func (s *Service) Verify(ctx context.Context, userID, code string) error {
	tx, err := s.Store.BeginVerify(ctx, userID)
	if err != nil {
		return fmt.Errorf("emailchange: opening verification: %w", err)
	}
	settled := false
	defer func() {
		if !settled {
			_ = tx.Rollback(ctx)
		}
	}()

	row, err := tx.Token(ctx)
	if err != nil {
		if errors.Is(err, ErrInvalidCode) {
			return ErrInvalidCode
		}
		return fmt.Errorf("emailchange: reading token: %w", err)
	}

	now := s.now()
	if row.UsedAt != nil || !row.ExpiresAt.After(now) {
		return ErrInvalidCode
	}
	if row.Attempts >= MaxAttempts {
		return ErrTooManyAttempts
	}

	if !security.CodesEqual(security.HashCode(code), row.CodeHash) {
		if err := tx.RecordFailedAttempt(ctx); err != nil {
			return fmt.Errorf("emailchange: recording attempt: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("emailchange: committing attempt: %w", err)
		}
		settled = true
		s.record(ctx, audit.Event{Name: audit.EventEmailChangeFailed, UserID: userID,
			Metadata: map[string]any{"reason": "wrong_code"}})
		return ErrInvalidCode
	}

	// The address may have been claimed between request and verification.
	taken, err := s.Store.AddressTaken(ctx, row.Target)
	if err != nil {
		return fmt.Errorf("emailchange: rechecking address: %w", err)
	}
	if taken {
		return ErrEmailTaken
	}

	if err := tx.SetEmail(ctx, userID, row.Target); err != nil {
		return fmt.Errorf("emailchange: setting email: %w", err)
	}
	if err := tx.Consume(ctx); err != nil {
		return fmt.Errorf("emailchange: consuming token: %w", err)
	}
	if err := tx.RevokeAllFamilies(ctx, userID); err != nil {
		return fmt.Errorf("emailchange: revoking sessions: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("emailchange: committing: %w", err)
	}
	settled = true

	s.record(ctx, audit.Event{Name: audit.EventEmailChanged, UserID: userID, Email: row.Target})
	return nil
}

func (s *Service) publish(ctx context.Context, message mailer.Message) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("emailchange: encoding mail: %w", err)
	}
	if err := s.Publisher.Publish(ctx, payload); err != nil {
		return fmt.Errorf("emailchange: queueing mail: %w", err)
	}
	return nil
}

func (s *Service) record(ctx context.Context, event audit.Event) {
	if s.Auditor == nil {
		return
	}
	if err := s.Auditor.Record(ctx, event); err != nil && s.Logger != nil {
		s.Logger.Warn("audit write failed", slog.String("event", event.Name), slog.String("error", err.Error()))
	}
}

func (s *Service) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

// window is the quota window, defaulting when the field is unset.
func (s *Service) window() time.Duration {
	if s.Quota.Window > 0 {
		return s.Quota.Window
	}
	return security.DefaultWindow
}

func normalize(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
