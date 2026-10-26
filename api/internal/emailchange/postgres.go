package emailchange

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

// PostgresStore persists the email change state.
type PostgresStore struct {
	Pool *pgxpool.Pool
}

// NewPostgresStore returns a Store backed by the given pool.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{Pool: pool}
}

// PasswordHash returns the stored hash, or "" for a federated-only account.
func (s *PostgresStore) PasswordHash(ctx context.Context, userID string) (string, error) {
	var hash *string
	err := s.Pool.QueryRow(ctx, `
		SELECT tx_password_hash FROM seg.tb_user
		WHERE co_user = $1::uuid AND dt_deleted_at IS NULL`, userID).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrInvalidPassword
	}
	if err != nil {
		return "", fmt.Errorf("selecting password hash: %w", err)
	}
	if hash == nil {
		return "", nil
	}
	return *hash, nil
}

// CurrentEmail returns the account's address.
func (s *PostgresStore) CurrentEmail(ctx context.Context, userID string) (string, error) {
	var email *string
	err := s.Pool.QueryRow(ctx, `
		SELECT tx_email FROM seg.tb_user
		WHERE co_user = $1::uuid AND dt_deleted_at IS NULL`, userID).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrInvalidPassword
	}
	if err != nil {
		return "", fmt.Errorf("selecting email: %w", err)
	}
	if email == nil {
		return "", nil
	}
	return *email, nil
}

// AddressTaken reports whether another active account owns the address. The model's unique
// index is partial, so a deleted account frees its address.
func (s *PostgresStore) AddressTaken(ctx context.Context, email string) (bool, error) {
	var taken bool
	err := s.Pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM seg.tb_user WHERE tx_email = $1 AND dt_deleted_at IS NULL
		)`, email).Scan(&taken)
	if err != nil {
		return false, fmt.Errorf("checking address: %w", err)
	}
	return taken, nil
}

// CreateChangeToken invalidates any pending change and stores the new one.
//
// Invalidating first is what makes "the account changed meanwhile" impossible to apply: only
// the newest code can ever be consumed, so a stale one cannot move the account later.
func (s *PostgresStore) CreateChangeToken(ctx context.Context, userID, target, codeHash string, expiresAt time.Time) error {
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
		UPDATE seg.tb_user_token
		SET dt_used_at = now(), dt_updated_at = now()
		WHERE co_user = $1::uuid AND tx_purpose = 'EMAIL_CHANGE' AND dt_used_at IS NULL`, userID); err != nil {
		return fmt.Errorf("invalidating previous tokens: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO seg.tb_user_token
			(co_user_token, co_user, tx_purpose, tx_code_hash, tx_target,
			 nu_attempts, dt_expires_at, dt_created_at, dt_updated_at)
		VALUES (gen_random_uuid(), $1::uuid, 'EMAIL_CHANGE', $2, $3, 0, $4, now(), now())`,
		userID, codeHash, target, expiresAt); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return ErrCodeCollision
		}
		return fmt.Errorf("inserting tb_user_token: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing token: %w", err)
	}
	settled = true
	return nil
}

// LastRequestAt returns when the user last asked for a change.
func (s *PostgresStore) LastRequestAt(ctx context.Context, userID string) (*time.Time, error) {
	var last *time.Time
	err := s.Pool.QueryRow(ctx, `
		SELECT max(dt_created_at) FROM seg.tb_user_token
		WHERE co_user = $1::uuid AND tx_purpose = 'EMAIL_CHANGE'`, userID).Scan(&last)
	if err != nil {
		return nil, fmt.Errorf("selecting last request: %w", err)
	}
	return last, nil
}

// CountRequestsSince counts the changes requested after the given instant.
func (s *PostgresStore) CountRequestsSince(ctx context.Context, userID string, since time.Time) (int, error) {
	var count int
	err := s.Pool.QueryRow(ctx, `
		SELECT count(*) FROM seg.tb_user_token
		WHERE co_user = $1::uuid AND tx_purpose = 'EMAIL_CHANGE' AND dt_created_at >= $2`,
		userID, since).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting requests: %w", err)
	}
	return count, nil
}

// BeginVerify opens the transaction for the newest unused token.
func (s *PostgresStore) BeginVerify(ctx context.Context, userID string) (VerifyTx, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	return &verifyTx{tx: tx, userID: userID}, nil
}

type verifyTx struct {
	tx     pgx.Tx
	userID string
	token  string
}

func (t *verifyTx) Token(ctx context.Context) (ChangeRow, error) {
	var row ChangeRow
	err := t.tx.QueryRow(ctx, `
		SELECT co_user_token::text, co_user::text, tx_code_hash, tx_target,
		       nu_attempts, dt_expires_at, dt_used_at
		FROM seg.tb_user_token
		WHERE co_user = $1::uuid AND tx_purpose = 'EMAIL_CHANGE' AND dt_used_at IS NULL
		ORDER BY dt_created_at DESC
		LIMIT 1
		FOR UPDATE`, t.userID,
	).Scan(&row.TokenID, &row.UserID, &row.CodeHash, &row.Target,
		&row.Attempts, &row.ExpiresAt, &row.UsedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return ChangeRow{}, ErrInvalidCode
	}
	if err != nil {
		return ChangeRow{}, fmt.Errorf("selecting token: %w", err)
	}
	t.token = row.TokenID
	return row, nil
}

func (t *verifyTx) RecordFailedAttempt(ctx context.Context) error {
	_, err := t.tx.Exec(ctx, `
		UPDATE seg.tb_user_token
		SET nu_attempts = nu_attempts + 1, dt_updated_at = now()
		WHERE co_user_token = $1::uuid`, t.token)
	if err != nil {
		return fmt.Errorf("incrementing attempts: %w", err)
	}
	return nil
}

func (t *verifyTx) Consume(ctx context.Context) error {
	_, err := t.tx.Exec(ctx, `
		UPDATE seg.tb_user_token
		SET dt_used_at = now(), dt_updated_at = now()
		WHERE co_user_token = $1::uuid`, t.token)
	if err != nil {
		return fmt.Errorf("consuming token: %w", err)
	}
	return nil
}

func (t *verifyTx) SetEmail(ctx context.Context, userID, newEmail string) error {
	_, err := t.tx.Exec(ctx, `
		UPDATE seg.tb_user
		SET tx_email = $2, dt_updated_at = now()
		WHERE co_user = $1::uuid`, userID, newEmail)
	if err != nil {
		return fmt.Errorf("updating email: %w", err)
	}
	return nil
}

func (t *verifyTx) RevokeAllFamilies(ctx context.Context, userID string) error {
	_, err := t.tx.Exec(ctx, `
		UPDATE seg.tb_refresh_token
		SET dt_revoked_at = now(), dt_updated_at = now()
		WHERE co_user = $1::uuid AND dt_revoked_at IS NULL`, userID)
	if err != nil {
		return fmt.Errorf("revoking sessions: %w", err)
	}
	return nil
}

func (t *verifyTx) Commit(ctx context.Context) error { return t.tx.Commit(ctx) }

func (t *verifyTx) Rollback(ctx context.Context) error { return t.tx.Rollback(ctx) }
