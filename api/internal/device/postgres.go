package device

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres stores the registrations.
type Postgres struct {
	Pool *pgxpool.Pool
}

// NewPostgresStore returns a Store backed by the given pool.
func NewPostgresStore(pool *pgxpool.Pool) *Postgres { return &Postgres{Pool: pool} }

// Register upserts by token.
//
// The conflict target names the partial unique index, not the column: the index only covers
// live rows, so a token that was retired can be inserted fresh instead of trying to update a
// tombstone. On conflict the row changes owner — a phone that changes hands must be reachable by
// whoever is holding it now, and leaving it bound to the previous account would deliver their
// notifications to a stranger.
func (p *Postgres) Register(ctx context.Context, userID, token, platform string) (Device, error) {
	const query = `
INSERT INTO seg.tb_device_token
    (co_device, co_user, tx_token, tx_platform, dt_last_seen, dt_created_at, dt_updated_at)
VALUES (gen_random_uuid(), $1::uuid, $2, $3, now(), now(), now())
ON CONFLICT (tx_token) WHERE dt_deleted_at IS NULL
DO UPDATE SET co_user      = excluded.co_user,
              tx_platform  = excluded.tx_platform,
              dt_last_seen = now(),
              dt_updated_at = now()
RETURNING co_device::text, tx_platform, dt_last_seen`

	var device Device
	if err := p.Pool.QueryRow(ctx, query, userID, token, platform).
		Scan(&device.ID, &device.Platform, &device.LastSeenAt); err != nil {
		return Device{}, err
	}
	return device, nil
}

// Unregister soft-deletes the device, but only when it belongs to the caller.
//
// The ownership predicate is in the WHERE clause rather than in a separate SELECT: checking
// first and deleting after is a race, and it would also confirm that the identifier exists.
func (p *Postgres) Unregister(ctx context.Context, userID, deviceID string) error {
	const query = `
UPDATE seg.tb_device_token
   SET dt_deleted_at = now(), dt_updated_at = now()
 WHERE co_device = $1::uuid
   AND co_user = $2::uuid
   AND dt_deleted_at IS NULL`

	tag, err := p.Pool.Exec(ctx, query, deviceID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Tokens returns the live registration tokens of one user, newest device first.
//
// This is the read the sender does before every push. It returns what exists; whether the user
// wants to be notified is decided one layer up, by the notification preferences, because a
// token is a transport and a preference is a decision.
func (p *Postgres) Tokens(ctx context.Context, userID string) ([]string, error) {
	rows, err := p.Pool.Query(ctx, `
		SELECT tx_token
		  FROM seg.tb_device_token
		 WHERE co_user = $1::uuid AND dt_deleted_at IS NULL
		 ORDER BY dt_last_seen DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tokens := make([]string, 0, 2)
	for rows.Next() {
		var token string
		if err := rows.Scan(&token); err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
	}
	return tokens, errors.Join(rows.Err())
}
