package password

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// uniqueViolation is the PostgreSQL code for a unique constraint breach.
const uniqueViolation = "23505"

// PostgresStore persists recovery state.
type PostgresStore struct {
	Pool *pgxpool.Pool
}

// NewPostgresStore returns a Store backed by the given pool.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{Pool: pool}
}

// ActiveUserID returns the user owning the address, or ErrNoUser.
func (s *PostgresStore) ActiveUserID(ctx context.Context, email string) (string, error) {
	var userID string
	err := s.Pool.QueryRow(ctx, `
		SELECT co_user::text FROM seg.tb_user
		WHERE tx_email = $1 AND dt_deleted_at IS NULL`, email).Scan(&userID)

	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoUser
	}
	if err != nil {
		return "", fmt.Errorf("selecting tb_user: %w", err)
	}
	return userID, nil
}

// CreateResetToken stores the hashed code. A collision on the unique hash is reported so the
// caller can draw another code instead of failing the request.
func (s *PostgresStore) CreateResetToken(ctx context.Context, userID, target, codeHash string, expiresAt time.Time) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO seg.tb_user_token
			(co_user_token, co_user, tx_purpose, tx_code_hash, tx_target,
			 nu_attempts, dt_expires_at, dt_created_at, dt_updated_at)
		VALUES (gen_random_uuid(), $1::uuid, 'PASSWORD_RESET', $2, $3, 0, $4, now(), now())`,
		userID, codeHash, target, expiresAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return ErrCodeCollision
		}
		return fmt.Errorf("inserting tb_user_token: %w", err)
	}
	return nil
}

// BeginReset opens the transaction for the newest unused token of the address.
func (s *PostgresStore) BeginReset(ctx context.Context, email string) (ResetTx, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	return &resetTx{tx: tx, email: email}, nil
}

// PasswordHash returns the stored hash, or "" for an account without a password.
func (s *PostgresStore) PasswordHash(ctx context.Context, userID string) (string, error) {
	var hash *string
	err := s.Pool.QueryRow(ctx, `
		SELECT tx_password_hash FROM seg.tb_user
		WHERE co_user = $1::uuid AND dt_deleted_at IS NULL`, userID).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoUser
	}
	if err != nil {
		return "", fmt.Errorf("selecting password hash: %w", err)
	}
	if hash == nil {
		return "", nil
	}
	return *hash, nil
}

// ChangePassword sets a new hash and revokes every family in one transaction.
func (s *PostgresStore) ChangePassword(ctx context.Context, userID, passwordHash string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	settled := false
	defer func() {
		if !settled {
			_ = tx.Rollback(ctx)
		}
	}()

	if _, err := tx.Exec(ctx, `
		UPDATE seg.tb_user
		SET tx_password_hash = $2, nu_failed_attempts = 0, dt_locked_until = NULL, dt_updated_at = now()
		WHERE co_user = $1::uuid`, userID, passwordHash); err != nil {
		return fmt.Errorf("updating password: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE seg.tb_refresh_token
		SET dt_revoked_at = now(), dt_updated_at = now()
		WHERE co_user = $1::uuid AND dt_revoked_at IS NULL`, userID); err != nil {
		return fmt.Errorf("revoking sessions: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing change: %w", err)
	}
	settled = true
	return nil
}

type resetTx struct {
	tx    pgx.Tx
	email string
	token string
}

func (t *resetTx) Token(ctx context.Context) (ResetRow, error) {
	var row ResetRow
	err := t.tx.QueryRow(ctx, `
		SELECT ut.co_user_token::text, ut.co_user::text, ut.tx_code_hash,
		       ut.nu_attempts, ut.dt_expires_at, ut.dt_used_at
		FROM seg.tb_user_token ut
		JOIN seg.tb_user u ON u.co_user = ut.co_user
		WHERE u.tx_email = $1 AND u.dt_deleted_at IS NULL
		  AND ut.tx_purpose = 'PASSWORD_RESET' AND ut.dt_used_at IS NULL
		ORDER BY ut.dt_created_at DESC
		LIMIT 1
		FOR UPDATE OF ut`, t.email,
	).Scan(&row.TokenID, &row.UserID, &row.CodeHash, &row.Attempts, &row.ExpiresAt, &row.UsedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return ResetRow{}, ErrInvalidCode
	}
	if err != nil {
		return ResetRow{}, fmt.Errorf("selecting reset token: %w", err)
	}
	t.token = row.TokenID
	return row, nil
}

func (t *resetTx) RecordFailedAttempt(ctx context.Context) error {
	_, err := t.tx.Exec(ctx, `
		UPDATE seg.tb_user_token
		SET nu_attempts = nu_attempts + 1, dt_updated_at = now()
		WHERE co_user_token = $1::uuid`, t.token)
	if err != nil {
		return fmt.Errorf("incrementing attempts: %w", err)
	}
	return nil
}

func (t *resetTx) Consume(ctx context.Context) error {
	_, err := t.tx.Exec(ctx, `
		UPDATE seg.tb_user_token
		SET dt_used_at = now(), dt_updated_at = now()
		WHERE co_user_token = $1::uuid`, t.token)
	if err != nil {
		return fmt.Errorf("consuming token: %w", err)
	}
	return nil
}

func (t *resetTx) SetPassword(ctx context.Context, userID, passwordHash string) error {
	_, err := t.tx.Exec(ctx, `
		UPDATE seg.tb_user
		SET tx_password_hash = $2, nu_failed_attempts = 0, dt_locked_until = NULL, dt_updated_at = now()
		WHERE co_user = $1::uuid`, userID, passwordHash)
	if err != nil {
		return fmt.Errorf("updating password: %w", err)
	}
	return nil
}

func (t *resetTx) RevokeAllFamilies(ctx context.Context, userID string) error {
	_, err := t.tx.Exec(ctx, `
		UPDATE seg.tb_refresh_token
		SET dt_revoked_at = now(), dt_updated_at = now()
		WHERE co_user = $1::uuid AND dt_revoked_at IS NULL`, userID)
	if err != nil {
		return fmt.Errorf("revoking sessions: %w", err)
	}
	return nil
}

func (t *resetTx) Commit(ctx context.Context) error { return t.tx.Commit(ctx) }

func (t *resetTx) Rollback(ctx context.Context) error { return t.tx.Rollback(ctx) }
