package places

import (
	"context"
	"fmt"

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
	sort     string
	page     int
	limit    int
}

// mapStoreQuery is the store-level map request.
type mapStoreQuery struct {
	box      geo.BBox
	country  string
	region   string
	category string
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
const listPredicate = `
FROM core.tb_place p
WHERE p.dt_deleted_at IS NULL
  AND p.tx_status = 'ACTIVE'
  AND ($2::char(2) IS NULL OR p.tx_country = $2::char(2))
  AND ($3::varchar IS NULL OR p.tx_region = $3::varchar)
  AND ($4::uuid IS NULL OR p.co_place_category = $4::uuid)
  AND ST_DWithin(p.co_location, $1::geography, $5)`

// listColumns is the projection of one list row, in scan order.
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
  AND ST_DWithin(p.co_location, $1::geography, $5)`

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

// List returns one page of places and the total under the same predicate.
func (s *PostgresStore) List(ctx context.Context, q listStoreQuery) ([]ListRow, int, error) {
	args := []any{ewktPoint(q.near), nullable(q.country), nullable(q.region), nullable(q.category), q.radius}

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
	rows, err := s.pool.Query(ctx, listColumns+order+" LIMIT $6 OFFSET $7", append(args, q.limit, offset)...)
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

// MapPoints returns the viewport rows, plus whether the row cap was hit.
func (s *PostgresStore) MapPoints(ctx context.Context, q mapStoreQuery) ([]MapPoint, bool, error) {
	const sql = `
SELECT p.co_place, p.nu_latitude, p.nu_longitude
FROM core.tb_place p
WHERE p.dt_deleted_at IS NULL
  AND p.tx_status = 'ACTIVE'
  AND ($5::char(2) IS NULL OR p.tx_country = $5::char(2))
  AND ($6::varchar IS NULL OR p.tx_region = $6::varchar)
  AND ($7::uuid IS NULL OR p.co_place_category = $7::uuid)
  AND p.co_location && ST_MakeEnvelope($1, $2, $3, $4, 4326)::geography
LIMIT $8`

	args := []any{
		q.box.MinLng, q.box.MinLat, q.box.MaxLng, q.box.MaxLat,
		nullable(q.country), nullable(q.region), nullable(q.category),
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
