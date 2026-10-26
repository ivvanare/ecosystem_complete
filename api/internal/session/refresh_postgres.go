package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresRotateStore opens rotation transactions.
type PostgresRotateStore struct {
	Pool *pgxpool.Pool
}

// NewPostgresRotateStore returns a RotateStore backed by the given pool.
func NewPostgresRotateStore(pool *pgxpool.Pool) *PostgresRotateStore {
	return &PostgresRotateStore{Pool: pool}
}

// BeginRotate starts a transaction bound to one token hash.
func (s *PostgresRotateStore) BeginRotate(ctx context.Context, tokenHash string) (RotateTx, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	return &rotateTx{tx: tx, hash: tokenHash}, nil
}

type rotateTx struct {
	tx   pgx.Tx
	hash string
}

func (t *rotateTx) Token(ctx context.Context) (TokenRow, error) {
	var row TokenRow
	err := t.tx.QueryRow(ctx, `
		SELECT co_user::text, co_token_family::text,
		       COALESCE(tx_device, ''), COALESCE(tx_user_agent, ''), COALESCE(tx_ip, ''),
		       dt_expires_at, dt_revoked_at
		FROM seg.tb_refresh_token
		WHERE tx_token_hash = $1
		FOR UPDATE`, t.hash,
	).Scan(&row.UserID, &row.Family, &row.Device, &row.UserAgent, &row.IP, &row.ExpiresAt, &row.RevokedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return TokenRow{}, ErrNoToken
	}
	if err != nil {
		return TokenRow{}, fmt.Errorf("selecting refresh token: %w", err)
	}
	return row, nil
}

func (t *rotateTx) Revoke(ctx context.Context) error {
	_, err := t.tx.Exec(ctx, `
		UPDATE seg.tb_refresh_token
		SET dt_revoked_at = now(), dt_updated_at = now()
		WHERE tx_token_hash = $1 AND dt_revoked_at IS NULL`, t.hash)
	if err != nil {
		return fmt.Errorf("revoking refresh token: %w", err)
	}
	return nil
}

// RevokeFamily revokes every live token of the family this token belongs to, located by
// subselect so the caller does not have to pass the family around.
func (t *rotateTx) RevokeFamily(ctx context.Context) error {
	_, err := t.tx.Exec(ctx, `
		UPDATE seg.tb_refresh_token
		SET dt_revoked_at = now(), dt_updated_at = now()
		WHERE co_token_family = (
			SELECT co_token_family FROM seg.tb_refresh_token WHERE tx_token_hash = $1
		) AND dt_revoked_at IS NULL`, t.hash)
	if err != nil {
		return fmt.Errorf("revoking token family: %w", err)
	}
	return nil
}

func (t *rotateTx) Insert(ctx context.Context, record RefreshRecord) error {
	_, err := t.tx.Exec(ctx, `
		INSERT INTO seg.tb_refresh_token
			(co_refresh_token, co_user, tx_token_hash, co_token_family, tx_device,
			 tx_user_agent, tx_ip, dt_expires_at, dt_created_at, dt_updated_at)
		VALUES (gen_random_uuid(), $1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), $7, now(), now())`,
		record.UserID, record.TokenHash, record.Family,
		record.Device, record.UserAgent, record.IP, record.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("inserting rotated token: %w", err)
	}
	return nil
}

func (t *rotateTx) Commit(ctx context.Context) error { return t.tx.Commit(ctx) }

func (t *rotateTx) Rollback(ctx context.Context) error { return t.tx.Rollback(ctx) }
