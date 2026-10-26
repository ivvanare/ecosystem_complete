package signup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/security"
	"github.com/ivvanare/ecosystem_complete/api/internal/session"
)

// Errors the handler maps onto the contract.
var (
	// ErrInvalidCode covers "no pending registration", "wrong code" and "expired": telling
	// them apart would confirm which addresses are mid-registration.
	ErrInvalidCode = errors.New("signup: code is invalid or expired")
	// ErrTooManyAttempts means the counter is spent; the registration is dead.
	ErrTooManyAttempts = errors.New("signup: too many attempts")
	// ErrNoPending is returned by a Tx when the email has no pending registration.
	ErrNoPending = errors.New("signup: no pending registration")
)

// MaxAttempts is the contract's ceiling for a 6-digit code.
const MaxAttempts = 5

// PendingRow is the locked row being verified.
type PendingRow struct {
	Name         string
	PasswordHash string
	CodeHash     string
	Attempts     int
	ExpiresAt    time.Time
}

// NewUser is the row promoted into seg.tb_user.
type NewUser struct {
	Name            string
	Email           string
	PasswordHash    string
	EmailVerifiedAt time.Time
}

// Tx is one transaction over the pending registration, already holding the row lock.
type Tx interface {
	// Pending returns the locked row, or ErrNoPending.
	Pending(ctx context.Context) (PendingRow, error)
	// RecordFailedAttempt increments nu_attempts.
	RecordFailedAttempt(ctx context.Context) error
	// Promote inserts the user and returns its id.
	Promote(ctx context.Context, user NewUser) (string, error)
	// DiscardPending deletes the consumed registration.
	DiscardPending(ctx context.Context) error
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// VerifierStore opens the transaction.
type VerifierStore interface {
	BeginVerify(ctx context.Context, email string) (Tx, error)
}

// TokenIssuer starts the session after a successful registration.
type TokenIssuer interface {
	Issue(ctx context.Context, userID string, meta session.Meta) (session.Tokens, error)
}

// Verifier consumes a signup code.
type Verifier struct {
	Store  VerifierStore
	Issuer TokenIssuer
	Now    func() time.Time
}

// NewVerifier returns a Verifier with the real clock.
func NewVerifier(store VerifierStore, issuer TokenIssuer) *Verifier {
	return &Verifier{Store: store, Issuer: issuer, Now: time.Now}
}

// Verify validates the code and promotes the pending registration into a user.
//
// The row is locked for the whole check, so two concurrent attempts cannot both consume the
// same code. A wrong code commits the incremented counter and returns ErrInvalidCode; a
// successful one deletes the pending row and issues the first session.
func (v *Verifier) Verify(ctx context.Context, email, code string, meta session.Meta) (session.Tokens, error) {
	email = normalizeEmail(email)

	tx, err := v.Store.BeginVerify(ctx, email)
	if err != nil {
		return session.Tokens{}, fmt.Errorf("signup: opening verification: %w", err)
	}

	settled := false
	defer func() {
		if !settled {
			_ = tx.Rollback(ctx)
		}
	}()

	pending, err := tx.Pending(ctx)
	if err != nil {
		if errors.Is(err, ErrNoPending) {
			return session.Tokens{}, ErrInvalidCode
		}
		return session.Tokens{}, fmt.Errorf("signup: reading pending registration: %w", err)
	}

	if !pending.ExpiresAt.After(v.now()) {
		return session.Tokens{}, ErrInvalidCode
	}
	if pending.Attempts >= MaxAttempts {
		return session.Tokens{}, ErrTooManyAttempts
	}

	if !security.CodesEqual(security.HashCode(code), pending.CodeHash) {
		if err := tx.RecordFailedAttempt(ctx); err != nil {
			return session.Tokens{}, fmt.Errorf("signup: recording failed attempt: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return session.Tokens{}, fmt.Errorf("signup: committing failed attempt: %w", err)
		}
		settled = true
		return session.Tokens{}, ErrInvalidCode
	}

	userID, err := tx.Promote(ctx, NewUser{
		Name:            pending.Name,
		Email:           email,
		PasswordHash:    pending.PasswordHash,
		EmailVerifiedAt: v.now(),
	})
	if err != nil {
		return session.Tokens{}, fmt.Errorf("signup: creating user: %w", err)
	}

	if err := tx.DiscardPending(ctx); err != nil {
		return session.Tokens{}, fmt.Errorf("signup: discarding pending registration: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return session.Tokens{}, fmt.Errorf("signup: committing verification: %w", err)
	}
	settled = true

	// Tokens are issued after the commit: if signing fails the user still exists and can log
	// in with the password they just chose, so a failure here is recoverable.
	tokens, err := v.Issuer.Issue(ctx, userID, meta)
	if err != nil {
		return session.Tokens{}, err
	}

	return tokens, nil
}

func (v *Verifier) now() time.Time {
	if v.Now == nil {
		return time.Now()
	}
	return v.Now()
}
