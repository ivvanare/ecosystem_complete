package signup

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresVerifier opens the verification transaction.
type PostgresVerifier struct {
	Pool *pgxpool.Pool
}

// NewPostgresVerifier returns a VerifierStore backed by the given pool.
func NewPostgresVerifier(pool *pgxpool.Pool) *PostgresVerifier {
	return &PostgresVerifier{Pool: pool}
}

// BeginVerify starts a transaction. The row is locked in Pending, not here, so the lock is
// held only for the work that needs it.
func (v *PostgresVerifier) BeginVerify(ctx context.Context, email string) (Tx, error) {
	tx, err := v.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	return &postgresTx{tx: tx, email: email}, nil
}

type postgresTx struct {
	tx    pgx.Tx
	email string
}

func (t *postgresTx) Pending(ctx context.Context) (PendingRow, error) {
	var row PendingRow
	err := t.tx.QueryRow(ctx, `
		SELECT tx_name, tx_password_hash, tx_code_hash, nu_attempts, dt_expires_at
		FROM seg.tb_signup_request
		WHERE tx_email = $1
		FOR UPDATE`, t.email,
	).Scan(&row.Name, &row.PasswordHash, &row.CodeHash, &row.Attempts, &row.ExpiresAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return PendingRow{}, ErrNoPending
	}
	if err != nil {
		return PendingRow{}, fmt.Errorf("selecting pending registration: %w", err)
	}
	return row, nil
}

func (t *postgresTx) RecordFailedAttempt(ctx context.Context) error {
	_, err := t.tx.Exec(ctx, `
		UPDATE seg.tb_signup_request
		SET nu_attempts = nu_attempts + 1, dt_updated_at = now()
		WHERE tx_email = $1`, t.email)
	if err != nil {
		return fmt.Errorf("incrementing attempts: %w", err)
	}
	return nil
}

func (t *postgresTx) Promote(ctx context.Context, user NewUser) (string, error) {
	var userID string
	err := t.tx.QueryRow(ctx, `
		INSERT INTO seg.tb_user
			(co_user, tx_name, tx_email, tx_password_hash, dt_email_verified_at,
			 tx_status, dt_created_at, dt_updated_at)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, 'ACTIVE', now(), now())
		RETURNING co_user::text`,
		user.Name, user.Email, user.PasswordHash, user.EmailVerifiedAt,
	).Scan(&userID)
	if err != nil {
		return "", fmt.Errorf("inserting tb_user: %w", err)
	}
	return userID, nil
}

func (t *postgresTx) DiscardPending(ctx context.Context) error {
	_, err := t.tx.Exec(ctx, `DELETE FROM seg.tb_signup_request WHERE tx_email = $1`, t.email)
	if err != nil {
		return fmt.Errorf("deleting pending registration: %w", err)
	}
	return nil
}

func (t *postgresTx) Commit(ctx context.Context) error { return t.tx.Commit(ctx) }

func (t *postgresTx) Rollback(ctx context.Context) error { return t.tx.Rollback(ctx) }
