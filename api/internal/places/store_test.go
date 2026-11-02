package places

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ivvanare/ecosystem_complete/api/internal/geo"
)

// The SQL shape is asserted against a fake that records the query, because these
// tests must not need PostgreSQL (p1-discovery.md §8, AGENTS.md). The live-database
// proof of the same statements runs separately, inside a transaction that rolls back.

type recordedQuery struct {
	sql  string
	args []any
}

type fakeQueryer struct {
	recorded []recordedQuery
	listRows [][]any
	mapRows  [][]any
	count    int
	queryErr error
	rowErr   error
}

func (f *fakeQueryer) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	f.recorded = append(f.recorded, recordedQuery{sql, args})
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	switch {
	case strings.Contains(sql, "ST_DWithin"):
		return &fakeRows{data: f.listRows}, nil
	case strings.Contains(sql, "ST_MakeEnvelope"):
		return &fakeRows{data: f.mapRows}, nil
	}
	return nil, fmt.Errorf("unexpected query: %s", sql)
}

func (f *fakeQueryer) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	f.recorded = append(f.recorded, recordedQuery{sql, args})
	return &fakeRow{value: f.count, err: f.rowErr}
}

type fakeRow struct {
	value int
	err   error
}

func (r *fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 1 {
		return fmt.Errorf("count scan with %d dest, want 1", len(dest))
	}
	target, ok := dest[0].(*int)
	if !ok {
		return fmt.Errorf("count scan dest is %T, want *int", dest[0])
	}
	*target = r.value
	return nil
}

type fakeRows struct {
	data [][]any
	idx  int
	err  error
}

func (r *fakeRows) Close()                                       {}
func (r *fakeRows) TypeMap() *pgtype.Map                         { return nil }
func (r *fakeRows) Err() error                                   { return r.err }
func (r *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRows) RawValues() [][]byte                          { return nil }
func (r *fakeRows) Conn() *pgx.Conn                              { return nil }
func (r *fakeRows) Values() ([]any, error)                       { return r.data[r.idx-1], nil }

func (r *fakeRows) Next() bool {
	if r.idx >= len(r.data) {
		return false
	}
	r.idx++
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	row := r.data[r.idx-1]
	if len(dest) != len(row) {
		return fmt.Errorf("scan %d dest for %d values", len(dest), len(row))
	}
	for i := range dest {
		target := reflect.ValueOf(dest[i])
		if target.Kind() != reflect.Pointer {
			return fmt.Errorf("scan dest %d is not a pointer", i)
		}
		if row[i] == nil {
			target.Elem().Set(reflect.Zero(target.Elem().Type()))
			continue
		}
		value := reflect.ValueOf(row[i])
		if target.Elem().Kind() == reflect.Pointer {
			allocated := reflect.New(target.Elem().Type().Elem())
			allocated.Elem().Set(value)
			target.Elem().Set(allocated)
			continue
		}
		if !value.Type().AssignableTo(target.Elem().Type()) {
			return fmt.Errorf("cannot assign %T to %s", row[i], target.Elem().Type())
		}
		target.Elem().Set(value)
	}
	return nil
}

var listFixture = [][]any{
	{"p1", "Arepera El Proof", "cat-1", "Arepera", "VE", "VE-A", 10.4806, -66.9036, "Calle 1", 120.4},
	{"p2", "Hotel Proof", "cat-2", "Hotel", "VE", nil, 10.4810, -66.9040, nil, 940.2},
}

func listQuery() listStoreQuery {
	return listStoreQuery{
		near:     geo.Coordinate{Lat: 10.4806, Lng: -66.9036},
		radius:   2000,
		country:  "VE",
		region:   "VE-A",
		category: "43c43189-8813-4f55-8636-f2aff3c37c5d",
		sort:     SortDistance,
		page:     2,
		limit:    20,
	}
}

func TestListCountsThenSelectsWithTheSamePredicate(t *testing.T) {
	fake := &fakeQueryer{listRows: listFixture, count: 42}
	store := &PostgresStore{pool: fake}

	rows, total, err := store.List(context.Background(), listQuery())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 42 {
		t.Errorf("total = %d, want 42", total)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}

	if len(fake.recorded) != 2 {
		t.Fatalf("recorded %d queries, want count + select", len(fake.recorded))
	}
	count, sel := fake.recorded[0], fake.recorded[1]

	// The spatial predicate is expressed on co_location, the derived column — or the
	// GiST index is not used and the query degrades silently to a scan (S16).
	for i, q := range []recordedQuery{count, sel} {
		if !strings.Contains(q.sql, "ST_DWithin(p.co_location, $1::geography, $5)") {
			t.Errorf("query %d does not filter with ST_DWithin on co_location:\n%s", i, q.sql)
		}
		if strings.Contains(q.sql, "ST_DWithin(ST_") {
			t.Errorf("query %d rebuilds the point from the decimal columns: the index is unusable:\n%s", i, q.sql)
		}
		for _, want := range []string{"dt_deleted_at IS NULL", "tx_status = 'ACTIVE'", "$2::char(2)", "$3::varchar", "$4::uuid"} {
			if !strings.Contains(q.sql, want) {
				t.Errorf("query %d misses %q:\n%s", i, want, q.sql)
			}
		}
	}
	if !strings.Contains(count.sql, "COUNT(*)") {
		t.Errorf("count query does not count:\n%s", count.sql)
	}

	// The request point is bound as one geography parameter, never string-built into
	// the statement.
	point, ok := count.args[0].(string)
	if !ok || point != "SRID=4326;POINT(-66.9036 10.4806)" {
		t.Errorf("point arg = %v, want EWKT SRID=4326;POINT(-66.9036 10.4806)", count.args[0])
	}
	if len(count.args) != 5 {
		t.Errorf("count args = %d, want 5 (point, country, region, category, radius)", len(count.args))
	}

	// KNN ordering: the index walks places in true distance order, so no sort of the
	// whole candidate set happens (p1-discovery.md §2).
	if !strings.Contains(sel.sql, "ORDER BY p.co_location <-> $1::geography") {
		t.Errorf("select does not order with the KNN operator:\n%s", sel.sql)
	}
	if !strings.Contains(sel.sql, "ST_Distance(p.co_location, $1::geography)") {
		t.Errorf("select does not compute the distance:\n%s", sel.sql)
	}
	if !strings.Contains(sel.sql, "LIMIT $6 OFFSET $7") {
		t.Errorf("select is not paginated:\n%s", sel.sql)
	}
	if got := sel.args[5]; got != 20 {
		t.Errorf("limit arg = %v, want 20", got)
	}
	if got := sel.args[6]; got != 20 {
		t.Errorf("offset arg = %v, want 20 (page 2 of 20)", got)
	}

	// The row mapping: public fields only, nullable columns as pointers, the legal
	// name nowhere.
	first, second := rows[0], rows[1]
	if first.ID != "p1" || first.Title != "Arepera El Proof" || first.CategoryName != "Arepera" {
		t.Errorf("first row = %+v", first)
	}
	if first.Region == nil || *first.Region != "VE-A" {
		t.Errorf("first.Region = %v, want VE-A", first.Region)
	}
	if first.DistanceM != 120.4 {
		t.Errorf("first.DistanceM = %v, want 120.4", first.DistanceM)
	}
	if second.Region != nil || second.Address != nil {
		t.Errorf("second row nullables = %v/%v, want nil", second.Region, second.Address)
	}
}

func TestListRatingOrdersByNameWhileNoAggregateExists(t *testing.T) {
	fake := &fakeQueryer{listRows: listFixture, count: 2}
	store := &PostgresStore{pool: fake}

	q := listQuery()
	q.sort = SortRating
	if _, _, err := store.List(context.Background(), q); err != nil {
		t.Fatalf("List: %v", err)
	}

	sel := fake.recorded[1]
	if !strings.Contains(sel.sql, "ORDER BY p.tx_title ASC") {
		t.Errorf("rating fallback does not order by name:\n%s", sel.sql)
	}
	if strings.Contains(sel.sql, "<->") {
		t.Errorf("rating fallback must not use the KNN operator:\n%s", sel.sql)
	}
	if strings.Contains(strings.ToLower(sel.sql), "tb_review") {
		t.Errorf("the read path must never aggregate tb_review in a request:\n%s", sel.sql)
	}
}

func TestMapPointsUsesTheEnvelopeOperatorAndTheCapProbe(t *testing.T) {
	fake := &fakeQueryer{mapRows: [][]any{
		{"p1", 10.4806, -66.9036},
		{"p2", 10.4810, -66.9040},
	}}
	store := &PostgresStore{pool: fake}

	q := mapStoreQuery{
		box:     geo.BBox{MinLng: -66.99, MinLat: 10.41, MaxLng: -66.82, MaxLat: 10.54},
		country: "VE",
	}
	points, truncated, err := store.MapPoints(context.Background(), q)
	if err != nil {
		t.Fatalf("MapPoints: %v", err)
	}
	if truncated {
		t.Error("truncated = true with 2 rows, want false")
	}
	if len(points) != 2 || points[0].ID != "p1" || points[1].Latitude != 10.4810 {
		t.Errorf("points = %+v", points)
	}

	if len(fake.recorded) != 1 {
		t.Fatalf("recorded %d queries, want 1", len(fake.recorded))
	}
	q0 := fake.recorded[0]
	if !strings.Contains(q0.sql, "p.co_location && ST_MakeEnvelope($1, $2, $3, $4, 4326)::geography") {
		t.Errorf("viewport predicate is not the envelope operator on co_location:\n%s", q0.sql)
	}
	if !strings.Contains(q0.sql, "LIMIT $8") {
		t.Errorf("viewport query is not capped:\n%s", q0.sql)
	}
	// Longitude first, like GeoJSON (p1-discovery.md §1).
	want := []any{-66.99, 10.41, -66.82, 10.54}
	for i, w := range want {
		if q0.args[i] != w {
			t.Errorf("arg %d = %v, want %v", i, q0.args[i], w)
		}
	}
	if got := q0.args[7]; got != MapRowCap+1 {
		t.Errorf("cap arg = %v, want %d (cap + 1 probe)", got, MapRowCap+1)
	}
}

func TestMapPointsReportsTruncationWhenTheProbeRowArrives(t *testing.T) {
	rows := make([][]any, 0, MapRowCap+1)
	for i := 0; i < MapRowCap+1; i++ {
		rows = append(rows, []any{fmt.Sprintf("p%d", i), 10.48, -66.9})
	}
	fake := &fakeQueryer{mapRows: rows}
	store := &PostgresStore{pool: fake}

	points, truncated, err := store.MapPoints(context.Background(), mapStoreQuery{})
	if err != nil {
		t.Fatalf("MapPoints: %v", err)
	}
	if !truncated {
		t.Error("truncated = false with the probe row present, want true")
	}
	if len(points) != MapRowCap {
		t.Errorf("points = %d, want the cap %d (the probe row is not a result)", len(points), MapRowCap)
	}
}

func TestStoreWrapsFailures(t *testing.T) {
	store := &PostgresStore{pool: &fakeQueryer{queryErr: errors.New("connection lost")}}
	if _, _, err := store.List(context.Background(), listQuery()); err == nil {
		t.Error("List returned nil error, want the failure wrapped")
	}
	if _, _, err := store.MapPoints(context.Background(), mapStoreQuery{}); err == nil {
		t.Error("MapPoints returned nil error, want the failure wrapped")
	}

	countBroken := &PostgresStore{pool: &fakeQueryer{rowErr: errors.New("count failed")}}
	if _, _, err := countBroken.List(context.Background(), listQuery()); err == nil {
		t.Error("List returned nil error on count failure, want it wrapped")
	}
}
