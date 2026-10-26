package login

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore reads users and records the lockout state.
type PostgresStore struct {
	Pool *pgxpool.Pool
}

// NewPostgresStore returns a Store backed by the given pool.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{Pool: pool}
}

// ByEmail returns the active user for the address, or ErrNotFound.
//
// Deleted accounts are excluded, matching the partial unique index that frees their email.
func (s *PostgresStore) ByEmail(ctx context.Context, email string) (User, error) {
	var user User
	err := s.Pool.QueryRow(ctx, `
		SELECT co_user::text, COALESCE(tx_email, ''), COALESCE(tx_password_hash, ''),
		       tx_status::text, nu_failed_attempts, dt_locked_until
		FROM seg.tb_user
		WHERE tx_email = $1 AND dt_deleted_at IS NULL`, email,
	).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Status, &user.FailedAttempts, &user.LockedUntil)

	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("selecting tb_user: %w", err)
	}
	return user, nil
}

// RecordFailure increments the counter and sets the lock when one is given.
func (s *PostgresStore) RecordFailure(ctx context.Context, userID string, lockUntil *time.Time) error {
	_, err := s.Pool.Exec(ctx, `
		UPDATE seg.tb_user
		SET nu_failed_attempts = nu_failed_attempts + 1,
		    dt_locked_until     = COALESCE($2, dt_locked_until),
		    dt_updated_at       = now()
		WHERE co_user = $1::uuid`, userID, lockUntil)
	if err != nil {
		return fmt.Errorf("updating failed attempts: %w", err)
	}
	return nil
}

// ResetFailures clears the counter and the lock after a successful login.
func (s *PostgresStore) ResetFailures(ctx context.Context, userID string) error {
	_, err := s.Pool.Exec(ctx, `
		UPDATE seg.tb_user
		SET nu_failed_attempts = 0, dt_locked_until = NULL, dt_updated_at = now()
		WHERE co_user = $1::uuid`, userID)
	if err != nil {
		return fmt.Errorf("resetting failed attempts: %w", err)
	}
	return nil
}
