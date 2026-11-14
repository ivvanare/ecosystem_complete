package trending

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresLoader reads the signals of every live place in one aggregate query.
//
// This is the only place in the service that aggregates core.tb_review, and it runs in the
// worker on a schedule — never inside a request (S24, D3). The window boundaries are
// computed by the database so the batch has one consistent notion of "now".
type PostgresLoader struct {
	pool *pgxpool.Pool
}

// NewPostgresLoader wires the loader.
func NewPostgresLoader(pool *pgxpool.Pool) *PostgresLoader { return &PostgresLoader{pool: pool} }

// signalsQuery counts recent reviews and the lifetime rating per place.
//
// Only ACTIVE, live reviews count: a moderated or deleted review must not feed a ranking,
// or moderation would be cosmetic. The FILTER clauses keep the two windows in one pass
// instead of three queries.
const signalsQuery = `
SELECT p.co_place::text,
       p.nu_latitude::float8,
       p.nu_longitude::float8,
       count(r.co_review) FILTER (WHERE r.dt_created_at > now() - interval '7 days')  AS reviews_7d,
       count(r.co_review) FILTER (WHERE r.dt_created_at > now() - interval '30 days') AS reviews_30d,
       coalesce(avg(r.nu_rating), 0)::float8                                          AS rating_avg,
       count(r.co_review)                                                             AS rating_n
FROM core.tb_place p
LEFT JOIN core.tb_review r
       ON r.co_place = p.co_place
      AND r.dt_deleted_at IS NULL
      AND r.tx_status = 'ACTIVE'
WHERE p.dt_deleted_at IS NULL
  AND p.tx_status = 'ACTIVE'
GROUP BY p.co_place, p.nu_latitude, p.nu_longitude`

// Signals returns one Signal per live place.
func (l *PostgresLoader) Signals(ctx context.Context) ([]Signal, error) {
	rows, err := l.pool.Query(ctx, signalsQuery)
	if err != nil {
		return nil, fmt.Errorf("querying signals: %w", err)
	}
	defer rows.Close()

	signals := make([]Signal, 0, 256)
	for rows.Next() {
		var signal Signal
		if err := rows.Scan(
			&signal.PlaceID, &signal.Latitude, &signal.Longitude,
			&signal.Reviews7d, &signal.Reviews30d, &signal.RatingAvg, &signal.RatingN,
		); err != nil {
			return nil, fmt.Errorf("scanning signal: %w", err)
		}
		signals = append(signals, signal)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("during signal iteration: %w", err)
	}
	return signals, nil
}
