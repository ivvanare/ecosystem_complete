package feed

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres reads the feed.
type Postgres struct {
	Pool *pgxpool.Pool
}

// NewPostgresStore returns a Store backed by the given pool.
func NewPostgresStore(pool *pgxpool.Pool) *Postgres { return &Postgres{Pool: pool} }

// feedQuery is the whole read: three sources merged by the database.
//
// A UNION ALL with a LIMIT, not three queries merged in Go: merging in memory would need a full
// page from each source before it could be sure the first page is right, which is three times the
// work for one answer — and it would still have to re-sort.
//
// Every branch carries the same 14 columns in the same order, NULL-padded, because that is what
// a UNION requires and because one scan then reads every kind.
//
// The visibility rules are the ones the sources already have. The feed is a shortcut to things
// the reader could reach by tapping; it is never a way to see something else.
const feedQuery = `
WITH followed AS (
    SELECT f.co_followed AS co_user
      FROM core.tb_follow f
     WHERE f.co_follower = $1::uuid
)
SELECT kind, at, actor_id, actor_name, actor_avatar,
       co_place, place_title,
       co_review, rating, comment,
       co_checkin,
       co_route, route_name, visibility, scheduled_at
  FROM (
        SELECT 'review'::text AS kind, r.dt_created_at AS at,
               r.co_user AS actor_id, u.tx_name AS actor_name, u.tx_avatar_url AS actor_avatar,
               r.co_place AS co_place, p.tx_title AS place_title,
               r.co_review AS co_review, r.nu_rating AS rating, r.tx_comment AS comment,
               NULL::uuid AS co_checkin,
               NULL::uuid AS co_route, NULL::text AS route_name,
               NULL::text AS visibility, NULL::timestamp AS scheduled_at,
               r.co_review AS tie_break
          FROM core.tb_review r
          JOIN followed f ON f.co_user = r.co_user
          JOIN seg.tb_user u ON u.co_user = r.co_user
          JOIN core.tb_place p ON p.co_place = r.co_place
                              AND p.dt_deleted_at IS NULL AND p.tx_status = 'ACTIVE'
         WHERE r.dt_deleted_at IS NULL AND r.tx_status = 'ACTIVE'

        UNION ALL

        SELECT 'checkin'::text, c.dt_created_at,
               c.co_user, u.tx_name, u.tx_avatar_url,
               c.co_place, p.tx_title,
               NULL::uuid, NULL::integer, NULL::text,
               c.co_checkin,
               NULL::uuid, NULL::text,
               NULL::text, NULL::timestamp,
               c.co_checkin
          FROM core.tb_checkin c
          JOIN followed f ON f.co_user = c.co_user
          JOIN seg.tb_user u ON u.co_user = c.co_user
          JOIN core.tb_place p ON p.co_place = c.co_place
                              AND p.dt_deleted_at IS NULL AND p.tx_status = 'ACTIVE'

        UNION ALL

        SELECT 'plan'::text, r.dt_created_at,
               r.co_user, u.tx_name, u.tx_avatar_url,
               NULL::uuid, NULL::text,
               NULL::uuid, NULL::integer, NULL::text,
               NULL::uuid,
               r.co_route, r.tx_name,
               r.tx_visibility::text, r.dt_scheduled_at,
               r.co_route
          FROM core.tb_route r
          JOIN followed f ON f.co_user = r.co_user
          JOIN seg.tb_user u ON u.co_user = r.co_user
         WHERE r.dt_deleted_at IS NULL
           AND r.tx_status = 'ACTIVE'
           -- A private plan is not an event the world gets to watch.
           AND r.tx_visibility = 'PUBLIC'
       ) merged
 WHERE NOT EXISTS (
        SELECT 1 FROM core.tb_block b
         WHERE (b.co_blocker = $1::uuid AND b.co_blocked = merged.actor_id)
            OR (b.co_blocker = merged.actor_id AND b.co_blocked = $1::uuid))
   AND ($2::timestamp IS NULL
        OR (merged.at, merged.tie_break) < ($2::timestamp, $3::uuid))
 ORDER BY merged.at DESC, merged.tie_break DESC
 LIMIT $4`

// Page runs the read. It returns at most q.Limit rows; the service asks for one more than it will
// show, so the probe that says "there is another page" costs nothing extra.
func (p *Postgres) Page(ctx context.Context, q Query) ([]Item, error) {
	var cursorAt any
	var cursorID any
	if q.Cursor != nil {
		cursorAt = q.Cursor.At.UTC()
		cursorID = q.Cursor.ID
	}

	rows, err := p.Pool.Query(ctx, feedQuery, q.Viewer, cursorAt, cursorID, q.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Item, 0, q.Limit)
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanItem(row scanner) (Item, error) {
	var (
		item        Item
		kind        string
		at          time.Time
		actorID     string
		actorName   *string
		actorAvatar *string
		placeID     *string
		placeTitle  *string
		coReview    *string
		rating      *int
		comment     *string
		coCheckin   *string
		coRoute     *string
		routeName   *string
		visibility  *string
		scheduledAt *time.Time
	)
	if err := row.Scan(&kind, &at, &actorID, &actorName, &actorAvatar,
		&placeID, &placeTitle, &coReview, &rating, &comment,
		&coCheckin, &coRoute, &routeName, &visibility, &scheduledAt); err != nil {
		return Item{}, err
	}

	item.Kind = kind
	item.At = at
	item.Actor = Actor{ID: actorID, Name: actorName, AvatarURL: actorAvatar}
	if placeID != nil {
		item.Place = &PlaceRef{ID: *placeID, Title: placeTitle}
	}
	if coReview != nil {
		item.CoReview = *coReview
	}
	if rating != nil {
		item.Rating = *rating
	}
	item.Comment = comment
	if coCheckin != nil {
		item.CoCheckin = *coCheckin
	}
	if coRoute != nil {
		item.Plan = &PlanRef{
			ID: *coRoute, Name: deref(routeName), Visibility: deref(visibility),
			ScheduledAt: scheduledAt,
		}
	}
	return item, nil
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
