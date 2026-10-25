package signup

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore is the real Store.
type PostgresStore struct {
	Pool *pgxpool.Pool
}

// NewPostgresStore returns a Store backed by the given pool.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{Pool: pool}
}

// ActiveUserExists reports whether a non-deleted user owns the email. The model's unique
// index is partial (`WHERE dt_deleted_at IS NULL`), so a deleted account frees its address.
func (s *PostgresStore) ActiveUserExists(ctx context.Context, email string) (bool, error) {
	var exists bool
	err := s.Pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM seg.tb_user
			WHERE tx_email = $1 AND dt_deleted_at IS NULL
		)`, email,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("querying tb_user: %w", err)
	}
	return exists, nil
}

// UpsertPending replaces any previous pending registration for the email.
//
// Replacing is deliberate: the email is immutable during signup, and asking again must
// issue a new code rather than let an old one linger. Attempts reset because the code is
// new.
func (s *PostgresStore) UpsertPending(ctx context.Context, pending Pending) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO seg.tb_signup_request
			(co_signup_request, tx_email, tx_name, tx_password_hash, tx_code_hash,
			 nu_attempts, dt_expires_at, dt_created_at, dt_updated_at)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, 0, $5, now(), now())
		ON CONFLICT (tx_email) DO UPDATE SET
			tx_name          = EXCLUDED.tx_name,
			tx_password_hash = EXCLUDED.tx_password_hash,
			tx_code_hash     = EXCLUDED.tx_code_hash,
			nu_attempts      = 0,
			dt_expires_at    = EXCLUDED.dt_expires_at,
			dt_updated_at    = now()`,
		pending.Email, pending.Name, pending.PasswordHash, pending.CodeHash, pending.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("upserting tb_signup_request: %w", err)
	}
	return nil
}
