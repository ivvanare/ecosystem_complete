package places

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

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
	if len(count.args) != 10 {
		t.Errorf("count args = %d, want 10 (point, country, region, category, radius, q, dish, attributes, maxPrice, openAt)", len(count.args))
	}

	// KNN ordering: the index walks places in true distance order, so no sort of the
	// whole candidate set happens (p1-discovery.md §2).
	if !strings.Contains(sel.sql, "ORDER BY p.co_location <-> $1::geography") {
		t.Errorf("select does not order with the KNN operator:\n%s", sel.sql)
	}
	if !strings.Contains(sel.sql, "ST_Distance(p.co_location, $1::geography)") {
		t.Errorf("select does not compute the distance:\n%s", sel.sql)
	}
	if !strings.Contains(sel.sql, "LIMIT $11 OFFSET $12") {
		t.Errorf("select is not paginated:\n%s", sel.sql)
	}
	if got := sel.args[10]; got != 20 {
		t.Errorf("limit arg = %v, want 20", got)
	}
	if got := sel.args[11]; got != 20 {
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
	if !strings.Contains(q0.sql, "LIMIT $13") {
		t.Errorf("viewport query is not capped:\n%s", q0.sql)
	}
	// Longitude first, like GeoJSON (p1-discovery.md §1).
	want := []any{-66.99, 10.41, -66.82, 10.54}
	for i, w := range want {
		if q0.args[i] != w {
			t.Errorf("arg %d = %v, want %v", i, q0.args[i], w)
		}
	}
	if got := q0.args[12]; got != MapRowCap+1 {
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

// searchFilterQuery returns a list query with every commit-3 filter set.
func searchFilterQuery() listStoreQuery {
	q := listQuery()
	q.q = "terraza"
	q.dish = "pepiada"
	q.attributes = []string{"terraza", "petfriendly"}
	maxPrice := 10.0
	q.maxPrice = &maxPrice
	openAt := time.Date(2026, 11, 8, 3, 30, 0, 0, time.UTC)
	q.openAt = &openAt
	return q
}

// The commit-3 filters (p1-discovery.md §12) compose with the spatial predicate
// in list mode: the count and the page carry the same WHERE, every predicate is
// optional through a NULL bind, and the GiST-served ST_DWithin is untouched.
func TestListComposesSearchFilters(t *testing.T) {
	fake := &fakeQueryer{listRows: listFixture, count: 3}
	store := &PostgresStore{pool: fake}

	if _, _, err := store.List(context.Background(), searchFilterQuery()); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(fake.recorded) != 2 {
		t.Fatalf("recorded %d queries, want count + select", len(fake.recorded))
	}

	for i, q := range fake.recorded {
		// The spatial predicate stays on co_location: the GiST index keeps
		// serving the radius read (S4/S16 of the change, repo rule).
		if !strings.Contains(q.sql, "ST_DWithin(p.co_location, $1::geography, $5)") {
			t.Errorf("query %d lost the ST_DWithin on co_location:\n%s", i, q.sql)
		}
		// q: the stored vector and the query vector come from the same
		// immutable-wrapper expression, with 'simple' so names never stem
		// into each other (S16).
		if !strings.Contains(q.sql, "p.search_name @@ plainto_tsquery('simple', public.unaccent_immutable($6::text))") {
			t.Errorf("query %d misses the name search through the immutable wrapper:\n%s", i, q.sql)
		}
		if strings.Contains(q.sql, "'spanish'") {
			t.Errorf("query %d uses a stemming configuration:\n%s", i, q.sql)
		}
		// dish: an EXISTS over menu -> item that returns PLACES, respecting
		// soft delete and ACTIVE on both levels (S17).
		for _, want := range []string{
			"FROM core.tb_menu m",
			"JOIN core.tb_menu_item mi ON mi.co_menu = m.co_menu",
			"m.dt_deleted_at IS NULL AND m.tx_status = 'ACTIVE'",
			"mi.dt_deleted_at IS NULL AND mi.tx_status = 'ACTIVE'",
			"mi.search_text @@ plainto_tsquery('simple', public.unaccent_immutable($7::text))",
		} {
			if !strings.Contains(q.sql, want) {
				t.Errorf("query %d misses %q (dish):\n%s", i, want, q.sql)
			}
		}
		// attributes: conjunctive by counting the intersection against the
		// requested cardinality — never a JOIN ... IN, which degrades to
		// "any of them" (S18).
		for _, want := range []string{
			"SELECT count(*) FROM core.tb_place_attribute_link l",
			"a.tx_code = ANY($8::text[])",
			"= cardinality($8::text[])",
			"a.dt_deleted_at IS NULL",
		} {
			if !strings.Contains(q.sql, want) {
				t.Errorf("query %d misses %q (conjunctive attributes):\n%s", i, want, q.sql)
			}
		}
		// maxPrice: USD only, at least one active item under the amount on an
		// active menu (S22). No currency parameter exists.
		for _, want := range []string{
			"JOIN core.tb_currency c ON c.co_currency = mi.co_currency",
			"c.tx_code = 'USD'",
			"mi.nu_price <= $9::numeric",
		} {
			if !strings.Contains(q.sql, want) {
				t.Errorf("query %d misses %q (maxPrice):\n%s", i, want, q.sql)
			}
		}
		// openAt: evaluated in the place's own timezone, with the normal AND
		// the overnight branch — without the second, a place open 20:00-02:00
		// reports closed at 23:00 (S19).
		for _, want := range []string{
			"FROM core.tb_place_schedule sc",
			"EXTRACT(ISODOW FROM ($10::timestamptz AT TIME ZONE p.tx_timezone))",
			"sc.dt_close_time > sc.dt_open_time",
			"BETWEEN sc.dt_open_time AND sc.dt_close_time",
			"sc.dt_close_time <= sc.dt_open_time",
		} {
			if !strings.Contains(q.sql, want) {
				t.Errorf("query %d misses %q (openAt):\n%s", i, want, q.sql)
			}
		}
	}

	// Argument order: the existing five, then the new five.
	args := fake.recorded[0].args
	if args[5] != "terraza" || args[6] != "pepiada" {
		t.Errorf("q/dish args = %v/%v", args[5], args[6])
	}
	codes, ok := args[7].([]string)
	if !ok || len(codes) != 2 || codes[0] != "terraza" || codes[1] != "petfriendly" {
		t.Errorf("attributes arg = %v, want [terraza petfriendly]", args[7])
	}
	if args[8] != 10.0 {
		t.Errorf("maxPrice arg = %v, want 10", args[8])
	}
	wantOpen := time.Date(2026, 11, 8, 3, 30, 0, 0, time.UTC)
	instant, ok := args[9].(time.Time)
	if !ok || !instant.Equal(wantOpen) {
		t.Errorf("openAt arg = %v, want %v", args[9], wantOpen)
	}
}

// Without the new filters the predicates stay but bind NULL, so the WHERE is
// exactly the old behaviour (the optional-filter shape of p1-discovery.md §2).
func TestListWithoutSearchFiltersBindsNulls(t *testing.T) {
	fake := &fakeQueryer{listRows: listFixture, count: 1}
	store := &PostgresStore{pool: fake}

	if _, _, err := store.List(context.Background(), listQuery()); err != nil {
		t.Fatalf("List: %v", err)
	}
	args := fake.recorded[0].args
	for i, name := range []string{"q", "dish", "attributes", "maxPrice", "openAt"} {
		if args[5+i] != nil {
			t.Errorf("%s arg = %v, want nil (no filter)", name, args[5+i])
		}
	}
}

// The same filters compose with the viewport predicate in map mode (S21).
func TestMapPointsComposesSearchFilters(t *testing.T) {
	fake := &fakeQueryer{mapRows: [][]any{{"p1", 10.4806, -66.9036}}}
	store := &PostgresStore{pool: fake}

	openAt := time.Date(2026, 11, 8, 3, 30, 0, 0, time.UTC)
	maxPrice := 10.0
	q := mapStoreQuery{
		box:        geo.BBox{MinLng: -66.99, MinLat: 10.41, MaxLng: -66.82, MaxLat: 10.54},
		q:          "terraza",
		dish:       "pepiada",
		attributes: []string{"terraza"},
		maxPrice:   &maxPrice,
		openAt:     &openAt,
	}
	if _, _, err := store.MapPoints(context.Background(), q); err != nil {
		t.Fatalf("MapPoints: %v", err)
	}

	if len(fake.recorded) != 1 {
		t.Fatalf("recorded %d queries, want 1", len(fake.recorded))
	}
	q0 := fake.recorded[0]
	if !strings.Contains(q0.sql, "p.co_location && ST_MakeEnvelope($1, $2, $3, $4, 4326)::geography") {
		t.Errorf("viewport predicate is not the envelope operator on co_location:\n%s", q0.sql)
	}
	for _, want := range []string{
		"p.search_name @@ plainto_tsquery('simple', public.unaccent_immutable($8::text))",
		"mi.search_text @@ plainto_tsquery('simple', public.unaccent_immutable($9::text))",
		"a.tx_code = ANY($10::text[])",
		"= cardinality($10::text[])",
		"c.tx_code = 'USD'",
		"mi.nu_price <= $11::numeric",
		"EXTRACT(ISODOW FROM ($12::timestamptz AT TIME ZONE p.tx_timezone))",
		"sc.dt_close_time <= sc.dt_open_time",
		"LIMIT $13",
	} {
		if !strings.Contains(q0.sql, want) {
			t.Errorf("viewport query misses %q:\n%s", want, q0.sql)
		}
	}
	if q0.args[7] != "terraza" || q0.args[8] != "pepiada" {
		t.Errorf("q/dish args = %v/%v", q0.args[7], q0.args[8])
	}
	if q0.args[10] != 10.0 {
		t.Errorf("maxPrice arg = %v, want 10", q0.args[10])
	}
	if instant, ok := q0.args[11].(time.Time); !ok || !instant.Equal(openAt) {
		t.Errorf("openAt arg = %v, want %v", q0.args[11], openAt)
	}
	if got := q0.args[12]; got != MapRowCap+1 {
		t.Errorf("cap arg = %v, want %d", got, MapRowCap+1)
	}
}
