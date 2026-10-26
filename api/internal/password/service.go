// Package password implements recovery and change of the account password.
//
// Two rules come from the model: a recovery code proves ownership of the mailbox and is
// stored only as a hash with an attempt counter, and any successful password change revokes
// every refresh family — a stolen session must not survive the recovery that was meant to
// stop it.
package password

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
	// ErrInvalidCode covers "no pending reset", "wrong code", "expired" and "already used".
	ErrInvalidCode = errors.New("password: code is invalid or expired")
	// ErrTooManyAttempts means the counter is spent.
	ErrTooManyAttempts = errors.New("password: too many attempts")
	// ErrNoUser means no active account owns the address.
	ErrNoUser = errors.New("password: no active user")
	// ErrInvalidCurrentPassword is a failed re-authentication.
	ErrInvalidCurrentPassword = errors.New("password: current password does not match")
	// ErrCodeCollision means the generated code already exists; the caller retries.
	ErrCodeCollision = errors.New("password: code collision")
)

// Constants from the model: short life, few attempts.
const (
	CodeLifetime = 10 * time.Minute
	MaxAttempts  = 5
	maxCodeTries = 3
)

// ResetRow is the locked token row.
type ResetRow struct {
	TokenID   string
	UserID    string
	CodeHash  string
	Attempts  int
	ExpiresAt time.Time
	UsedAt    *time.Time
}

// ResetTx is one transaction over the recovery token.
type ResetTx interface {
	Token(ctx context.Context) (ResetRow, error)
	RecordFailedAttempt(ctx context.Context) error
	Consume(ctx context.Context) error
	SetPassword(ctx context.Context, userID, passwordHash string) error
	RevokeAllFamilies(ctx context.Context, userID string) error
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// Store persists recovery state.
type Store interface {
	// ActiveUserID returns the user owning the address, or ErrNoUser.
	ActiveUserID(ctx context.Context, email string) (string, error)
	// CreateResetToken stores a hashed code, or returns ErrCodeCollision.
	CreateResetToken(ctx context.Context, userID, target, codeHash string, expiresAt time.Time) error
	// BeginReset opens the transaction for the newest unused token of that address.
	BeginReset(ctx context.Context, email string) (ResetTx, error)
	// PasswordHash returns the stored hash for a user.
	PasswordHash(ctx context.Context, userID string) (string, error)
	// ChangePassword sets a new hash and revokes every family, atomically.
	ChangePassword(ctx context.Context, userID, passwordHash string) error
}

// Publisher queues the recovery mail.
type Publisher interface {
	Publish(ctx context.Context, payload []byte) error
}

// Service handles recovery and change.
type Service struct {
	Store     Store
	Publisher Publisher
	Auditor   audit.Recorder
	Logger    *slog.Logger

	Now            func() time.Time
	GenerateCode   func() (string, error)
	HashPassword   func(string) (string, error)
	VerifyPassword func(encoded, plain string) (bool, error)
}

// New returns a Service with real entropy and hashing.
func New(store Store, publisher Publisher, auditor audit.Recorder, logger *slog.Logger) *Service {
	return &Service{
		Store:          store,
		Publisher:      publisher,
		Auditor:        auditor,
		Logger:         logger,
		Now:            time.Now,
		GenerateCode:   security.GenerateCode,
		HashPassword:   security.HashPassword,
		VerifyPassword: security.VerifyPassword,
	}
}

// RequestReset mails a recovery code.
//
// It succeeds silently for an unknown address: answering differently would confirm which
// emails have accounts. No token is created and no mail is sent in that case.
func (s *Service) RequestReset(ctx context.Context, email string) error {
	email = normalizeEmail(email)

	userID, err := s.Store.ActiveUserID(ctx, email)
	if errors.Is(err, ErrNoUser) {
		s.record(ctx, audit.Event{Name: audit.EventPasswordResetRequested, Email: email,
			Metadata: map[string]any{"delivered": false}})
		return nil
	}
	if err != nil {
		return fmt.Errorf("password: looking up user: %w", err)
	}

	var code string
	for attempt := 0; attempt < maxCodeTries; attempt++ {
		code, err = s.GenerateCode()
		if err != nil {
			return fmt.Errorf("password: generating code: %w", err)
		}

		err = s.Store.CreateResetToken(ctx, userID, email, security.HashCode(code), s.now().Add(CodeLifetime))
		if err == nil {
			break
		}
		if !errors.Is(err, ErrCodeCollision) {
			return fmt.Errorf("password: storing reset token: %w", err)
		}
		// The six digits already exist for another account; draw again.
	}
	if err != nil {
		return fmt.Errorf("password: no free code after %d tries: %w", maxCodeTries, err)
	}

	if err := s.publish(ctx, mailer.Message{
		To:      email,
		Subject: "Recuperá tu contraseña",
		Text: fmt.Sprintf("Tu código para cambiar la contraseña es %s.\n"+
			"Vence en 10 minutos y sirve una sola vez. Si no lo pediste, ignorá este mensaje: "+
			"tu contraseña actual sigue funcionando.", code),
	}); err != nil {
		return err
	}

	s.record(ctx, audit.Event{Name: audit.EventPasswordResetRequested, UserID: userID, Email: email,
		Metadata: map[string]any{"delivered": true}})
	return nil
}

// Reset consumes the code, sets the new password and ends every session.
func (s *Service) Reset(ctx context.Context, email, code, newPassword string) error {
	email = normalizeEmail(email)

	tx, err := s.Store.BeginReset(ctx, email)
	if err != nil {
		return fmt.Errorf("password: opening reset: %w", err)
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
		return fmt.Errorf("password: reading reset token: %w", err)
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
			return fmt.Errorf("password: recording failed attempt: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("password: committing failed attempt: %w", err)
		}
		settled = true
		s.record(ctx, audit.Event{Name: audit.EventPasswordResetFailed, UserID: row.UserID, Email: email,
			Metadata: map[string]any{"reason": "wrong_code"}})
		return ErrInvalidCode
	}

	hash, err := s.HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("password: hashing: %w", err)
	}

	if err := tx.SetPassword(ctx, row.UserID, hash); err != nil {
		return fmt.Errorf("password: setting password: %w", err)
	}
	if err := tx.Consume(ctx); err != nil {
		return fmt.Errorf("password: consuming token: %w", err)
	}
	if err := tx.RevokeAllFamilies(ctx, row.UserID); err != nil {
		return fmt.Errorf("password: revoking sessions: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("password: committing reset: %w", err)
	}
	settled = true

	s.record(ctx, audit.Event{Name: audit.EventPasswordReset, UserID: row.UserID, Email: email})
	return nil
}

// Change sets a new password for an authenticated user after re-checking the current one.
func (s *Service) Change(ctx context.Context, userID, currentPassword, newPassword string) error {
	stored, err := s.Store.PasswordHash(ctx, userID)
	if err != nil {
		return fmt.Errorf("password: reading current hash: %w", err)
	}
	if stored == "" {
		// Federated-only account: there is no password to change.
		return ErrInvalidCurrentPassword
	}

	ok, err := s.VerifyPassword(stored, currentPassword)
	if err != nil || !ok {
		s.record(ctx, audit.Event{Name: audit.EventPasswordChangeFailed, UserID: userID,
			Metadata: map[string]any{"reason": "wrong_current_password"}})
		return ErrInvalidCurrentPassword
	}

	hash, err := s.HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("password: hashing: %w", err)
	}

	if err := s.Store.ChangePassword(ctx, userID, hash); err != nil {
		return fmt.Errorf("password: storing new password: %w", err)
	}

	s.record(ctx, audit.Event{Name: audit.EventPasswordChanged, UserID: userID})
	return nil
}

func (s *Service) publish(ctx context.Context, message mailer.Message) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("password: encoding mail: %w", err)
	}
	if err := s.Publisher.Publish(ctx, payload); err != nil {
		return fmt.Errorf("password: queueing mail: %w", err)
	}
	return nil
}

// record is best effort: the audit log never decides whether an operation succeeded.
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

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
