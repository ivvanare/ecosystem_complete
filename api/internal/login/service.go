// Package login authenticates a user with email and password.
//
// The response is deliberately uniform: an unknown address, a wrong password and an
// inactive account all answer the same error, and an unknown address still pays for an
// Argon2id verification so timing cannot be used to enumerate accounts.
package login

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/audit"
	"github.com/ivvanare/ecosystem_complete/api/internal/security"
	"github.com/ivvanare/ecosystem_complete/api/internal/session"
)

// ErrInvalidCredentials is the only failure the caller can distinguish.
var ErrInvalidCredentials = errors.New("login: invalid credentials")

// ErrNotFound is returned by a Store when no active user owns the address.
var ErrNotFound = errors.New("login: user not found")

// LockedError means the account is temporarily blocked after repeated failures.
type LockedError struct {
	RetryAfter time.Duration
}

func (e *LockedError) Error() string {
	return fmt.Sprintf("login: account locked for %s", e.RetryAfter.Round(time.Second))
}

// Defaults for the lockout.
const (
	DefaultMaxAttempts  = 5
	DefaultLockDuration = 15 * time.Minute
	statusActive        = "ACTIVE"
)

// User is what the store returns.
type User struct {
	ID             string
	Email          string
	PasswordHash   string
	Status         string
	FailedAttempts int
	LockedUntil    *time.Time
}

// Store reads the user and records the lockout state.
type Store interface {
	ByEmail(ctx context.Context, email string) (User, error)
	RecordFailure(ctx context.Context, userID string, lockUntil *time.Time) error
	ResetFailures(ctx context.Context, userID string) error
}

// Issuer opens the session.
type Issuer interface {
	Issue(ctx context.Context, userID string, meta session.Meta) (session.Tokens, error)
}

// Service authenticates a user.
type Service struct {
	Store   Store
	Issuer  Issuer
	Auditor audit.Recorder
	Logger  *slog.Logger

	Now            func() time.Time
	VerifyPassword func(encoded, plain string) (bool, error)
	MaxAttempts    int
	LockDuration   time.Duration
}

// New returns a Service with the real clock and hashing wired in.
func New(store Store, issuer Issuer, auditor audit.Recorder, logger *slog.Logger) *Service {
	return &Service{
		Store:          store,
		Issuer:         issuer,
		Auditor:        auditor,
		Logger:         logger,
		Now:            time.Now,
		VerifyPassword: security.VerifyPassword,
		MaxAttempts:    DefaultMaxAttempts,
		LockDuration:   DefaultLockDuration,
	}
}

// dummyHash is verified when the address is unknown, so a miss costs the same as a hit.
var dummyHash = sync.OnceValue(func() string {
	hash, err := security.HashPassword("dummy-password-that-is-never-valid")
	if err != nil {
		return ""
	}
	return hash
})

// Login returns a token pair or one of ErrInvalidCredentials / *LockedError.
func (s *Service) Login(ctx context.Context, email, password string, meta session.Meta) (session.Tokens, error) {
	email = normalizeEmail(email)

	user, err := s.Store.ByEmail(ctx, email)
	if errors.Is(err, ErrNotFound) {
		_, _ = s.VerifyPassword(dummyHash(), password)
		s.record(ctx, audit.Event{
			Name:      audit.EventLoginFailed,
			Email:     email,
			IP:        meta.IP,
			UserAgent: meta.UserAgent,
			Metadata:  map[string]any{"reason": "unknown_account"},
		})
		return session.Tokens{}, ErrInvalidCredentials
	}
	if err != nil {
		return session.Tokens{}, fmt.Errorf("login: reading user: %w", err)
	}

	now := s.now()

	if user.LockedUntil != nil && now.Before(*user.LockedUntil) {
		retry := user.LockedUntil.Sub(now)
		s.record(ctx, audit.Event{
			Name:      audit.EventLoginLocked,
			UserID:    user.ID,
			Email:     email,
			IP:        meta.IP,
			UserAgent: meta.UserAgent,
			Metadata:  map[string]any{"retry_after_seconds": int(retry.Seconds()), "reason": "still_locked"},
		})
		return session.Tokens{}, &LockedError{RetryAfter: retry}
	}

	if user.Status != statusActive {
		// A suspended account must not be distinguishable from a wrong password.
		_, _ = s.VerifyPassword(user.PasswordHash, password)
		s.record(ctx, audit.Event{
			Name:      audit.EventLoginFailed,
			UserID:    user.ID,
			Email:     email,
			IP:        meta.IP,
			UserAgent: meta.UserAgent,
			Metadata:  map[string]any{"reason": "account_not_active"},
		})
		return session.Tokens{}, ErrInvalidCredentials
	}

	ok, verifyErr := s.VerifyPassword(user.PasswordHash, password)
	if verifyErr != nil || !ok {
		return s.failAttempt(ctx, user, email, meta, now, verifyErr)
	}

	if err := s.Store.ResetFailures(ctx, user.ID); err != nil {
		return session.Tokens{}, fmt.Errorf("login: resetting failures: %w", err)
	}

	tokens, err := s.Issuer.Issue(ctx, user.ID, meta)
	if err != nil {
		return session.Tokens{}, err
	}

	s.record(ctx, audit.Event{
		Name:      audit.EventLoginSucceeded,
		UserID:    user.ID,
		Email:     email,
		IP:        meta.IP,
		UserAgent: meta.UserAgent,
	})
	s.record(ctx, audit.Event{
		Name:      audit.EventSessionCreated,
		UserID:    user.ID,
		IP:        meta.IP,
		UserAgent: meta.UserAgent,
	})

	return tokens, nil
}

func (s *Service) failAttempt(ctx context.Context, user User, email string, meta session.Meta, now time.Time, verifyErr error) (session.Tokens, error) {
	failures := user.FailedAttempts + 1

	var lockUntil *time.Time
	if failures >= s.maxAttempts() {
		until := now.Add(s.lockDuration())
		lockUntil = &until
	}

	if err := s.Store.RecordFailure(ctx, user.ID, lockUntil); err != nil {
		return session.Tokens{}, fmt.Errorf("login: recording failure: %w", err)
	}

	if lockUntil != nil {
		s.record(ctx, audit.Event{
			Name:      audit.EventLoginLocked,
			UserID:    user.ID,
			Email:     email,
			IP:        meta.IP,
			UserAgent: meta.UserAgent,
			Metadata:  map[string]any{"attempts": failures, "retry_after_seconds": int(s.lockDuration().Seconds())},
		})
		return session.Tokens{}, &LockedError{RetryAfter: s.lockDuration()}
	}

	metadata := map[string]any{"reason": "wrong_password", "attempts": failures}
	if verifyErr != nil {
		metadata["reason"] = "unreadable_hash"
	}
	s.record(ctx, audit.Event{
		Name:      audit.EventLoginFailed,
		UserID:    user.ID,
		Email:     email,
		IP:        meta.IP,
		UserAgent: meta.UserAgent,
		Metadata:  metadata,
	})

	return session.Tokens{}, ErrInvalidCredentials
}

// record is best effort: an audit write must never turn a successful login into an error.
func (s *Service) record(ctx context.Context, event audit.Event) {
	if s.Auditor == nil {
		return
	}
	if err := s.Auditor.Record(ctx, event); err != nil {
		if s.Logger != nil {
			s.Logger.Warn("audit write failed",
				slog.String("event", event.Name),
				slog.String("error", err.Error()),
			)
		}
	}
}

func (s *Service) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

func (s *Service) maxAttempts() int {
	if s.MaxAttempts <= 0 {
		return DefaultMaxAttempts
	}
	return s.MaxAttempts
}

func (s *Service) lockDuration() time.Duration {
	if s.LockDuration <= 0 {
		return DefaultLockDuration
	}
	return s.LockDuration
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
