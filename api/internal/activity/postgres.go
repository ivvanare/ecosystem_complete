package activity

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres reads the projection from the tables the product already writes.
type Postgres struct {
	Pool *pgxpool.Pool
	// now is a field so a test can pin the clock; production uses time.Now.
	now func() time.Time
}

// NewPostgresStore returns a Store backed by the given pool.
func NewPostgresStore(pool *pgxpool.Pool) *Postgres {
	return &Postgres{Pool: pool, now: func() time.Time { return time.Now().UTC() }}
}

// Snapshot runs three reads: the windowed counters, the catalogue size, and the feed.
func (s *Postgres) Snapshot(ctx context.Context) (Snapshot, error) {
	snapshot := Snapshot{GeneratedAt: s.now()}

	if err := s.counters(ctx, &snapshot); err != nil {
		return Snapshot{}, err
	}
	if err := s.totals(ctx, &snapshot); err != nil {
		return Snapshot{}, err
	}
	if err := s.recent(ctx, &snapshot); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

// counters fills the three windows in one query with FILTER aggregates: the intervals are fixed by
// the contract, so three sequential scans over the same table would be three times the work for the
// same answer. Each source is bounded by the longest window, which is what keeps it an index range
// scan instead of a full table count.
func (s *Postgres) counters(ctx context.Context, snapshot *Snapshot) error {
	const query = `
WITH span AS (SELECT interval '5 minutes' AS short, interval '1 hour' AS medium,
                     interval '24 hours' AS long),
signups AS (
    SELECT count(*) FILTER (WHERE u.dt_created_at >= now() - span.short)  AS s5,
           count(*) FILTER (WHERE u.dt_created_at >= now() - span.medium) AS s1h,
           count(*)                                                       AS s24h
      FROM seg.tb_user u, span
     WHERE u.dt_deleted_at IS NULL AND u.dt_created_at >= now() - span.long
),
logins AS (
    SELECT count(*) FILTER (WHERE e.dt_created_at >= now() - span.short)  AS l5,
           count(*) FILTER (WHERE e.dt_created_at >= now() - span.medium) AS l1h,
           count(*)                                                       AS l24h
      FROM seg.tb_security_event e, span
     WHERE e.tx_event = 'auth.login.succeeded' AND e.dt_created_at >= now() - span.long
),
plans AS (
    SELECT count(*) FILTER (WHERE r.dt_created_at >= now() - span.short)  AS p5,
           count(*) FILTER (WHERE r.dt_created_at >= now() - span.medium) AS p1h,
           count(*)                                                       AS p24h
      FROM core.tb_route r, span
     WHERE r.dt_deleted_at IS NULL AND r.dt_created_at >= now() - span.long
)
SELECT signups.s5, signups.s1h, signups.s24h,
       logins.l5, logins.l1h, logins.l24h,
       plans.p5, plans.p1h, plans.p24h
  FROM signups, logins, plans`

	windows := []Window{{Label: "5m"}, {Label: "1h"}, {Label: "24h"}}
	if err := s.Pool.QueryRow(ctx, query).Scan(
		&windows[0].Signups, &windows[1].Signups, &windows[2].Signups,
		&windows[0].Logins, &windows[1].Logins, &windows[2].Logins,
		&windows[0].PlansCreated, &windows[1].PlansCreated, &windows[2].PlansCreated,
	); err != nil {
		return fmt.Errorf("counting activity: %w", err)
	}
	snapshot.Windows = windows
	return nil
}

// totals is the size of the catalogue, live rows only. A review that was moderated out does not
// count: the dashboard shows what a client can see.
func (s *Postgres) totals(ctx context.Context, snapshot *Snapshot) error {
	const query = `
SELECT (SELECT count(*) FROM seg.tb_user   WHERE dt_deleted_at IS NULL),
       (SELECT count(*) FROM core.tb_place  WHERE dt_deleted_at IS NULL),
       (SELECT count(*) FROM core.tb_route  WHERE dt_deleted_at IS NULL),
       (SELECT count(*) FROM core.tb_review WHERE dt_deleted_at IS NULL AND tx_status = 'ACTIVE')`
	if err := s.Pool.QueryRow(ctx, query).Scan(
		&snapshot.Totals.Users, &snapshot.Totals.Places,
		&snapshot.Totals.Plans, &snapshot.Totals.Reviews,
	); err != nil {
		return fmt.Errorf("counting totals: %w", err)
	}
	return nil
}

// recent is the feed: the newest rows of each kind, merged and capped.
func (s *Postgres) recent(ctx context.Context, snapshot *Snapshot) error {
	const query = `
SELECT kind, at FROM (
    SELECT 'signup' AS kind, u.dt_created_at AS at
      FROM seg.tb_user u
     WHERE u.dt_deleted_at IS NULL
    UNION ALL
    SELECT 'login' AS kind, e.dt_created_at AS at
      FROM seg.tb_security_event e
     WHERE e.tx_event = 'auth.login.succeeded'
    UNION ALL
    SELECT 'plan' AS kind, r.dt_created_at AS at
      FROM core.tb_route r
     WHERE r.dt_deleted_at IS NULL
) events
ORDER BY at DESC
LIMIT 20`

	rows, err := s.Pool.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("reading the feed: %w", err)
	}
	defer rows.Close()

	feed := make([]Recent, 0, 20)
	for rows.Next() {
		var row Recent
		if err := rows.Scan(&row.Kind, &row.At); err != nil {
			return fmt.Errorf("scanning the feed: %w", err)
		}
		feed = append(feed, row)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("reading the feed: %w", err)
	}
	snapshot.Recent = feed
	return nil
}
