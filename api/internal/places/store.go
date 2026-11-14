package places

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ivvanare/ecosystem_complete/api/internal/geo"
)

// MapRowCap bounds the viewport query (p1-discovery.md §2). The SQL asks for one row
// more as a probe: receiving it means the result was truncated, which the response
// must say rather than presenting sample counts as totals.
const MapRowCap = 5000

// listStoreQuery is the store-level list request: filters already validated, sort
// already resolved (a degraded trending arrives as SortRating).
type listStoreQuery struct {
	near     geo.Coordinate
	radius   int
	country  string
	region   string
	category string
	// q, dish, attributes, maxPrice and openAt are the commit-3 filters
	// (p1-discovery.md §12): empty/nil binds mean "no filter" through the
	// NULL-optional predicate shape.
	q          string
	dish       string
	attributes []string
	maxPrice   *float64
	openAt     *time.Time
	sort       string
	page       int
	limit      int
}

// mapStoreQuery is the store-level map request.
type mapStoreQuery struct {
	box        geo.BBox
	country    string
	region     string
	category   string
	q          string
	dish       string
	attributes []string
	maxPrice   *float64
	openAt     *time.Time
}

// queryer is the pgx seam: *pgxpool.Pool satisfies it and tests record it, because
// the suite never needs PostgreSQL (AGENTS.md).
type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// PostgresStore runs the discovery queries. Both spatial predicates are expressed on
// co_location — the derived column — or the GiST index is not used and the query
// degrades silently to a scan (S16, p1-discovery.md §2).
type PostgresStore struct {
	pool queryer
}

// NewPostgresStore returns a Store backed by the given pool.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// listPredicate is the shared WHERE clause of both list-mode statements: the count and
// the page must agree, so the filter is written once and interpolated into both.
//
// The point is bound as one EWKT parameter and every predicate reads co_location: the
// decimals are the contract, but rebuilding the point from them per row would make the
// GiST index unusable (S16).
//
// $6-$10 are the search and filter predicates of p1-discovery.md §12, each optional
// through a NULL bind. Two rules keep them honest: the stored tsvector and the query
// tsquery run through the SAME immutable wrapper so accent folding always agrees
// (S16), and the attribute filter counts the intersection against the requested
// cardinality — a JOIN ... IN would silently degrade "all of them" into "any of
// them" (S18). openAt keeps both the normal and the overnight branch, evaluated in
// the place's own timezone (S19); maxPrice matches USD-priced items only, with no
// currency parameter (S22).
const listPredicate = `
FROM core.tb_place p
WHERE p.dt_deleted_at IS NULL
  AND p.tx_status = 'ACTIVE'
  AND ($2::char(2) IS NULL OR p.tx_country = $2::char(2))
  AND ($3::varchar IS NULL OR p.tx_region = $3::varchar)
  AND ($4::uuid IS NULL OR p.co_place_category = $4::uuid)
  AND ST_DWithin(p.co_location, $1::geography, $5)
  AND ($6::text IS NULL OR p.search_name @@ plainto_tsquery('simple', public.unaccent_immutable($6::text)))
  AND ($7::text IS NULL OR EXISTS (
    SELECT 1
    FROM core.tb_menu m
    JOIN core.tb_menu_item mi ON mi.co_menu = m.co_menu
    WHERE m.co_place = p.co_place
      AND m.dt_deleted_at IS NULL AND m.tx_status = 'ACTIVE'
      AND mi.dt_deleted_at IS NULL AND mi.tx_status = 'ACTIVE'
      AND mi.search_text @@ plainto_tsquery('simple', public.unaccent_immutable($7::text))))
  AND ($8::text[] IS NULL OR (
    SELECT count(*) FROM core.tb_place_attribute_link l
    JOIN core.tb_place_attribute a ON a.co_attribute = l.co_attribute
    WHERE l.co_place = p.co_place
      AND a.tx_code = ANY($8::text[])
      AND a.dt_deleted_at IS NULL) = cardinality($8::text[]))
  AND ($9::numeric IS NULL OR EXISTS (
    SELECT 1
    FROM core.tb_menu m
    JOIN core.tb_menu_item mi ON mi.co_menu = m.co_menu
    JOIN core.tb_currency c ON c.co_currency = mi.co_currency
    WHERE m.co_place = p.co_place
      AND m.dt_deleted_at IS NULL AND m.tx_status = 'ACTIVE'
      AND mi.dt_deleted_at IS NULL AND mi.tx_status = 'ACTIVE'
      AND c.tx_code = 'USD'
      AND mi.nu_price <= $9::numeric))
  AND ($10::timestamptz IS NULL OR EXISTS (
    SELECT 1 FROM core.tb_place_schedule sc
    WHERE sc.co_place = p.co_place
      AND sc.nu_day_of_week = EXTRACT(ISODOW FROM ($10::timestamptz AT TIME ZONE p.tx_timezone))
      AND (
        (sc.dt_close_time > sc.dt_open_time
          AND ($10::timestamptz AT TIME ZONE p.tx_timezone)::time BETWEEN sc.dt_open_time AND sc.dt_close_time)
        OR
        (sc.dt_close_time <= sc.dt_open_time
          AND (($10::timestamptz AT TIME ZONE p.tx_timezone)::time >= sc.dt_open_time
            OR ($10::timestamptz AT TIME ZONE p.tx_timezone)::time <= sc.dt_close_time))
      )))`

// listColumns is the projection of one list row, in scan order. The WHERE clause
// must stay word-for-word identical to listPredicate: the count and the page
// describe the same set.
const listColumns = `
SELECT p.co_place, p.tx_title, p.co_place_category, c.tx_name,
       p.tx_country, p.tx_region, p.nu_latitude, p.nu_longitude, p.tx_address,
       ST_Distance(p.co_location, $1::geography) AS distance_m
FROM core.tb_place p
JOIN core.tb_place_category c ON c.co_place_category = p.co_place_category
WHERE p.dt_deleted_at IS NULL
  AND p.tx_status = 'ACTIVE'
  AND ($2::char(2) IS NULL OR p.tx_country = $2::char(2))
  AND ($3::varchar IS NULL OR p.tx_region = $3::varchar)
  AND ($4::uuid IS NULL OR p.co_place_category = $4::uuid)
  AND ST_DWithin(p.co_location, $1::geography, $5)
  AND ($6::text IS NULL OR p.search_name @@ plainto_tsquery('simple', public.unaccent_immutable($6::text)))
  AND ($7::text IS NULL OR EXISTS (
    SELECT 1
    FROM core.tb_menu m
    JOIN core.tb_menu_item mi ON mi.co_menu = m.co_menu
    WHERE m.co_place = p.co_place
      AND m.dt_deleted_at IS NULL AND m.tx_status = 'ACTIVE'
      AND mi.dt_deleted_at IS NULL AND mi.tx_status = 'ACTIVE'
      AND mi.search_text @@ plainto_tsquery('simple', public.unaccent_immutable($7::text))))
  AND ($8::text[] IS NULL OR (
    SELECT count(*) FROM core.tb_place_attribute_link l
    JOIN core.tb_place_attribute a ON a.co_attribute = l.co_attribute
    WHERE l.co_place = p.co_place
      AND a.tx_code = ANY($8::text[])
      AND a.dt_deleted_at IS NULL) = cardinality($8::text[]))
  AND ($9::numeric IS NULL OR EXISTS (
    SELECT 1
    FROM core.tb_menu m
    JOIN core.tb_menu_item mi ON mi.co_menu = m.co_menu
    JOIN core.tb_currency c ON c.co_currency = mi.co_currency
    WHERE m.co_place = p.co_place
      AND m.dt_deleted_at IS NULL AND m.tx_status = 'ACTIVE'
      AND mi.dt_deleted_at IS NULL AND mi.tx_status = 'ACTIVE'
      AND c.tx_code = 'USD'
      AND mi.nu_price <= $9::numeric))
  AND ($10::timestamptz IS NULL OR EXISTS (
    SELECT 1 FROM core.tb_place_schedule sc
    WHERE sc.co_place = p.co_place
      AND sc.nu_day_of_week = EXTRACT(ISODOW FROM ($10::timestamptz AT TIME ZONE p.tx_timezone))
      AND (
        (sc.dt_close_time > sc.dt_open_time
          AND ($10::timestamptz AT TIME ZONE p.tx_timezone)::time BETWEEN sc.dt_open_time AND sc.dt_close_time)
        OR
        (sc.dt_close_time <= sc.dt_open_time
          AND (($10::timestamptz AT TIME ZONE p.tx_timezone)::time >= sc.dt_open_time
            OR ($10::timestamptz AT TIME ZONE p.tx_timezone)::time <= sc.dt_close_time))
      )))`

// ewk tPoint renders the request coordinate as one EWKT literal so it travels as a
// single bound parameter and is never string-built into the statement (S1).
func ewktPoint(c geo.Coordinate) string {
	return fmt.Sprintf("SRID=4326;POINT(%g %g)", c.Lng, c.Lat)
}

// nullable turns an absent filter into a NULL bind, which the predicates above read as
// "no filter" rather than an empty-string comparison.
func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// nullableStrings is nullable for the attribute-code array.
func nullableStrings(values []string) any {
	if len(values) == 0 {
		return nil
	}
	return values
}

// nullableFloat and nullableTime dereference the optional filters: nil stays a
// NULL bind, a present value travels as the value, not a pointer.
func nullableFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}

// List returns one page of places and the total under the same predicate.
func (s *PostgresStore) List(ctx context.Context, q listStoreQuery) ([]ListRow, int, error) {
	args := []any{
		ewktPoint(q.near),
		nullable(q.country), nullable(q.region), nullable(q.category), q.radius,
		nullable(q.q), nullable(q.dish), nullableStrings(q.attributes),
		nullableFloat(q.maxPrice), nullableTime(q.openAt),
	}

	var total int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*)"+listPredicate, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting places: %w", err)
	}

	// Trending is resolved before the store is reached: a degraded request arrives as
	// SortRating, so this path never orders by a ranking (S6). Distance ordering uses
	// the KNN operator, which lets the index walk places in true distance order
	// instead of sorting the whole candidate set (p1-discovery.md §2).
	order := " ORDER BY p.co_location <-> $1::geography"
	if q.sort == SortRating {
		order = " ORDER BY p.tx_title ASC"
	}

	offset := (q.page - 1) * q.limit
	rows, err := s.pool.Query(ctx, listColumns+order+" LIMIT $11 OFFSET $12", append(args, q.limit, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("selecting places: %w", err)
	}
	defer rows.Close()

	result := make([]ListRow, 0, q.limit)
	for rows.Next() {
		var row ListRow
		if err := rows.Scan(
			&row.ID, &row.Title, &row.CategoryID, &row.CategoryName,
			&row.Country, &row.Region, &row.Latitude, &row.Longitude, &row.Address,
			&row.DistanceM,
		); err != nil {
			return nil, 0, fmt.Errorf("scanning place: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("during place iteration: %w", err)
	}
	return result, total, nil
}

// byIDsColumns is the projection of a ranked page: the same public fields as a list row,
// selected by explicit ids so the ranking decides the order, not the database.
const byIDsQuery = `
SELECT p.co_place, p.tx_title, p.co_place_category, c.tx_name,
       p.tx_country, p.tx_region, p.nu_latitude, p.nu_longitude, p.tx_address,
       ST_Distance(p.co_location, $1::geography) AS distance_m
FROM core.tb_place p
JOIN core.tb_place_category c ON c.co_place_category = p.co_place_category
WHERE p.dt_deleted_at IS NULL
  AND p.tx_status = 'ACTIVE'
  AND p.co_place = ANY($2::uuid[])`

// ByIDs reads the places of one ranked page. Rows come back in any order: the caller
// applies the ranking, which is the authority on order (p1-discovery.md §2).
func (s *PostgresStore) ByIDs(ctx context.Context, near geo.Coordinate, ids []string) ([]ListRow, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, byIDsQuery, ewktPoint(near), ids)
	if err != nil {
		return nil, fmt.Errorf("selecting ranked places: %w", err)
	}
	defer rows.Close()

	result := make([]ListRow, 0, len(ids))
	for rows.Next() {
		var row ListRow
		if err := rows.Scan(
			&row.ID, &row.Title, &row.CategoryID, &row.CategoryName,
			&row.Country, &row.Region, &row.Latitude, &row.Longitude, &row.Address,
			&row.DistanceM,
		); err != nil {
			return nil, fmt.Errorf("scanning ranked place: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("during ranked iteration: %w", err)
	}
	return result, nil
}

// MapPoints returns the viewport rows, plus whether the row cap was hit.
func (s *PostgresStore) MapPoints(ctx context.Context, q mapStoreQuery) ([]MapPoint, bool, error) {
	// $8-$12 are the same §12 filters as the list predicate, valid in both modes
	// (S21); the viewport read stays on co_location so the GiST index serves it.
	const sql = `
SELECT p.co_place, p.nu_latitude, p.nu_longitude
FROM core.tb_place p
WHERE p.dt_deleted_at IS NULL
  AND p.tx_status = 'ACTIVE'
  AND ($5::char(2) IS NULL OR p.tx_country = $5::char(2))
  AND ($6::varchar IS NULL OR p.tx_region = $6::varchar)
  AND ($7::uuid IS NULL OR p.co_place_category = $7::uuid)
  AND p.co_location && ST_MakeEnvelope($1, $2, $3, $4, 4326)::geography
  AND ($8::text IS NULL OR p.search_name @@ plainto_tsquery('simple', public.unaccent_immutable($8::text)))
  AND ($9::text IS NULL OR EXISTS (
    SELECT 1
    FROM core.tb_menu m
    JOIN core.tb_menu_item mi ON mi.co_menu = m.co_menu
    WHERE m.co_place = p.co_place
      AND m.dt_deleted_at IS NULL AND m.tx_status = 'ACTIVE'
      AND mi.dt_deleted_at IS NULL AND mi.tx_status = 'ACTIVE'
      AND mi.search_text @@ plainto_tsquery('simple', public.unaccent_immutable($9::text))))
  AND ($10::text[] IS NULL OR (
    SELECT count(*) FROM core.tb_place_attribute_link l
    JOIN core.tb_place_attribute a ON a.co_attribute = l.co_attribute
    WHERE l.co_place = p.co_place
      AND a.tx_code = ANY($10::text[])
      AND a.dt_deleted_at IS NULL) = cardinality($10::text[]))
  AND ($11::numeric IS NULL OR EXISTS (
    SELECT 1
    FROM core.tb_menu m
    JOIN core.tb_menu_item mi ON mi.co_menu = m.co_menu
    JOIN core.tb_currency c ON c.co_currency = mi.co_currency
    WHERE m.co_place = p.co_place
      AND m.dt_deleted_at IS NULL AND m.tx_status = 'ACTIVE'
      AND mi.dt_deleted_at IS NULL AND mi.tx_status = 'ACTIVE'
      AND c.tx_code = 'USD'
      AND mi.nu_price <= $11::numeric))
  AND ($12::timestamptz IS NULL OR EXISTS (
    SELECT 1 FROM core.tb_place_schedule sc
    WHERE sc.co_place = p.co_place
      AND sc.nu_day_of_week = EXTRACT(ISODOW FROM ($12::timestamptz AT TIME ZONE p.tx_timezone))
      AND (
        (sc.dt_close_time > sc.dt_open_time
          AND ($12::timestamptz AT TIME ZONE p.tx_timezone)::time BETWEEN sc.dt_open_time AND sc.dt_close_time)
        OR
        (sc.dt_close_time <= sc.dt_open_time
          AND (($12::timestamptz AT TIME ZONE p.tx_timezone)::time >= sc.dt_open_time
            OR ($12::timestamptz AT TIME ZONE p.tx_timezone)::time <= sc.dt_close_time))
      )))
LIMIT $13`

	args := []any{
		q.box.MinLng, q.box.MinLat, q.box.MaxLng, q.box.MaxLat,
		nullable(q.country), nullable(q.region), nullable(q.category),
		nullable(q.q), nullable(q.dish), nullableStrings(q.attributes),
		nullableFloat(q.maxPrice), nullableTime(q.openAt),
		MapRowCap + 1, // the probe row that reveals truncation
	}

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, false, fmt.Errorf("selecting viewport places: %w", err)
	}
	defer rows.Close()

	points := make([]MapPoint, 0, MapRowCap)
	for rows.Next() {
		var point MapPoint
		if err := rows.Scan(&point.ID, &point.Latitude, &point.Longitude); err != nil {
			return nil, false, fmt.Errorf("scanning viewport place: %w", err)
		}
		points = append(points, point)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("during viewport iteration: %w", err)
	}

	// The probe row is not a result: drop it and report that counts describe a sample.
	if len(points) > MapRowCap {
		return points[:MapRowCap], true, nil
	}
	return points, false, nil
}
