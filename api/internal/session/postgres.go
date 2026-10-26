package session

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresRefreshStore persists refresh tokens in seg.tb_refresh_token.
type PostgresRefreshStore struct {
	Pool *pgxpool.Pool
}

// NewPostgresRefreshStore returns a RefreshStore backed by the given pool.
func NewPostgresRefreshStore(pool *pgxpool.Pool) *PostgresRefreshStore {
	return &PostgresRefreshStore{Pool: pool}
}

// StoreRefreshToken inserts the hashed token and its family.
func (s *PostgresRefreshStore) StoreRefreshToken(ctx context.Context, record RefreshRecord) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO seg.tb_refresh_token
			(co_refresh_token, co_user, tx_token_hash, co_token_family, tx_device,
			 tx_user_agent, tx_ip, dt_expires_at, dt_created_at, dt_updated_at)
		VALUES (gen_random_uuid(), $1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), $7, now(), now())`,
		record.UserID, record.TokenHash, record.Family,
		record.Device, record.UserAgent, record.IP, record.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("inserting tb_refresh_token: %w", err)
	}
	return nil
}
