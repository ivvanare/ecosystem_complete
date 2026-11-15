package profile

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres reads the public profile.
type Postgres struct {
	Pool *pgxpool.Pool
}

// NewPostgresStore returns a Store backed by the given pool.
func NewPostgresStore(pool *pgxpool.Pool) *Postgres { return &Postgres{Pool: pool} }

// Profile runs one statement: the account and its four counters.
//
// The counters are scalar subqueries rather than five round trips, and each one is a count over
// an indexed foreign key. A profile is read far more often than a person writes, so paying five
// queries per screen would be paying it forever.
//
// Visibility is `ACTIVE` and not deleted. A suspended account is not a profile with a warning
// banner: to a stranger it does not exist, and answering 404 for it is what keeps a
// moderation decision private.
func (p *Postgres) Profile(ctx context.Context, userID string) (Profile, error) {
	const query = `
SELECT u.co_user::text, u.tx_name, u.dt_created_at,
       (SELECT count(*) FROM core.tb_review r
         WHERE r.co_user = u.co_user AND r.tx_status = 'ACTIVE' AND r.dt_deleted_at IS NULL),
       (SELECT count(*) FROM core.tb_checkin c WHERE c.co_user = u.co_user),
       (SELECT count(*) FROM core.tb_follow f WHERE f.co_followed = u.co_user),
       (SELECT count(*) FROM core.tb_follow f WHERE f.co_follower = u.co_user)
  FROM seg.tb_user u
 WHERE u.co_user = $1::uuid
   AND u.dt_deleted_at IS NULL
   AND u.tx_status = 'ACTIVE'`

	var result Profile
	if err := p.Pool.QueryRow(ctx, query, userID).Scan(
		&result.ID, &result.Name, &result.MemberSince,
		&result.Counts.Reviews, &result.Counts.Checkins,
		&result.Counts.Followers, &result.Counts.Following,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Profile{}, ErrNotFound
		}
		return Profile{}, err
	}
	return result, nil
}
