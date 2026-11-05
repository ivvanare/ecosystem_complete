package places

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/geo"
)

type fakeStore struct {
	listRows  []ListRow
	listTotal int
	listErr   error
	lastList  listStoreQuery
	mapPts    []MapPoint
	truncated bool
	mapErr    error
	lastMap   mapStoreQuery
}

func (f *fakeStore) List(_ context.Context, q listStoreQuery) ([]ListRow, int, error) {
	f.lastList = q
	return f.listRows, f.listTotal, f.listErr
}

func (f *fakeStore) MapPoints(_ context.Context, q mapStoreQuery) ([]MapPoint, bool, error) {
	f.lastMap = q
	return f.mapPts, f.truncated, f.mapErr
}

type fakeStats struct {
	stats map[string]Stats
	err   error
	ids   []string
}

func (f *fakeStats) PlaceStats(_ context.Context, ids []string) (map[string]Stats, error) {
	f.ids = ids
	return f.stats, f.err
}

type fakeRanking struct {
	ids  []string
	ok   bool
	err  error
	zone string
}

func (f *fakeRanking) Trending(_ context.Context, zone string) ([]string, bool, error) {
	f.zone = zone
	return f.ids, f.ok, f.err
}

func newTestService(store Store, stats StatsReader, ranking RankingReader) *Service {
	return NewService(store, stats, ranking, slog.New(slog.DiscardHandler))
}

var nearQuery = ListQuery{
	Near:   geo.Coordinate{Lat: 10.4806, Lng: -66.9036},
	Radius: geo.DefaultRadius,
	Sort:   SortDistance,
	Page:   1,
	Limit:  20,
}

func TestListDistanceMergesStatsAndReportsWhatRan(t *testing.T) {
	store := &fakeStore{
		listRows: []ListRow{
			{ID: "p1", Title: "Con rating", DistanceM: 120.4},
			{ID: "p2", Title: "Sin rating", DistanceM: 940.2},
		},
		listTotal: 2,
	}
	stats := &fakeStats{stats: map[string]Stats{"p1": {Average: 4.6, Count: 128}}}
	service := newTestService(store, stats, NoRanking{})

	result, err := service.List(context.Background(), nearQuery)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if result.SortApplied != SortDistance || result.Degraded {
		t.Errorf("SortApplied=%q Degraded=%v, want distance/false", result.SortApplied, result.Degraded)
	}
	if result.Total != 2 || len(result.Rows) != 2 {
		t.Fatalf("Total=%d Rows=%d, want 2/2", result.Total, len(result.Rows))
	}

	// §8 item 8: rating is the published aggregate when it exists, nil (never 0)
	// when the worker published nothing.
	withRating := result.Rows[0]
	if withRating.Rating == nil || withRating.Rating.Average != 4.6 || withRating.Rating.Count != 128 {
		t.Errorf("Rows[0].Rating = %+v, want {4.6 128}", withRating.Rating)
	}
	if result.Rows[1].Rating != nil {
		t.Errorf("Rows[1].Rating = %+v, want nil when no aggregate was published", result.Rows[1].Rating)
	}

	// The aggregate read asks only for the page's ids.
	if len(stats.ids) != 2 || stats.ids[0] != "p1" || stats.ids[1] != "p2" {
		t.Errorf("stats queried for %v, want [p1 p2]", stats.ids)
	}
}

func TestListTrendingFallsBackToRatingWhenNoZoneRanking(t *testing.T) {
	// §8 item 7: sort=trending with no zone key → rating order, degraded: true, and a
	// 200-class answer, never an error (S4/S6).
	store := &fakeStore{listRows: []ListRow{{ID: "p1", Title: "Alfa"}}, listTotal: 1}
	ranking := &fakeRanking{ok: false}
	service := newTestService(store, &fakeStats{}, ranking)

	q := nearQuery
	q.Sort = SortTrending
	result, err := service.List(context.Background(), q)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if result.SortApplied != SortRating {
		t.Errorf("SortApplied = %q, want %q (the order that actually ran)", result.SortApplied, SortRating)
	}
	if !result.Degraded {
		t.Error("Degraded = false, want true: the requested trending had no zone ranking")
	}
	if store.lastList.sort != SortRating {
		t.Errorf("store queried with sort %q, want the rating fallback", store.lastList.sort)
	}

	// The ranking was probed with the zone cell of the request coordinate, at the
	// worker's resolution — never with the raw coordinate.
	wantZone := cellString(t, q.Near.Lat, q.Near.Lng, geo.TrendingZoneResolution)
	if ranking.zone != wantZone {
		t.Errorf("ranking probed with zone %q, want %q", ranking.zone, wantZone)
	}
}

func TestListTrendingRankingErrorDegradesLikeAMiss(t *testing.T) {
	// A failing ranking read is a degraded answer, never an error (S4).
	store := &fakeStore{listRows: []ListRow{{ID: "p1"}}, listTotal: 1}
	ranking := &fakeRanking{err: errors.New("redis down")}
	service := newTestService(store, &fakeStats{}, ranking)

	q := nearQuery
	q.Sort = SortTrending
	result, err := service.List(context.Background(), q)
	if err != nil {
		t.Fatalf("List returned error %v, want a degraded answer", err)
	}
	if result.SortApplied != SortRating || !result.Degraded {
		t.Errorf("SortApplied=%q Degraded=%v, want rating/true", result.SortApplied, result.Degraded)
	}
}

func TestListRatingSortPassesThrough(t *testing.T) {
	store := &fakeStore{listRows: []ListRow{{ID: "p1"}}, listTotal: 1}
	service := newTestService(store, &fakeStats{}, NoRanking{})

	q := nearQuery
	q.Sort = SortRating
	result, err := service.List(context.Background(), q)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if result.SortApplied != SortRating || result.Degraded {
		t.Errorf("SortApplied=%q Degraded=%v, want rating/false", result.SortApplied, result.Degraded)
	}
	if store.lastList.sort != SortRating {
		t.Errorf("store queried with sort %q, want rating", store.lastList.sort)
	}
}

func TestListPropagatesStoreFailure(t *testing.T) {
	store := &fakeStore{listErr: errors.New("boom")}
	service := newTestService(store, &fakeStats{}, NoRanking{})

	if _, err := service.List(context.Background(), nearQuery); err == nil {
		t.Fatal("List returned nil error, want the store failure wrapped")
	}
}

func TestListStatsFailureDegradesToNullRatings(t *testing.T) {
	// The aggregate read failing must not fail discovery: ratings come back null.
	store := &fakeStore{listRows: []ListRow{{ID: "p1"}}, listTotal: 1}
	service := newTestService(store, &fakeStats{err: errors.New("redis down")}, NoRanking{})

	result, err := service.List(context.Background(), nearQuery)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if result.Rows[0].Rating != nil {
		t.Errorf("Rows[0].Rating = %+v, want nil on aggregate failure", result.Rows[0].Rating)
	}
}

func TestMapClustersTheReturnedRows(t *testing.T) {
	store := &fakeStore{mapPts: clusterFixture, truncated: false}
	service := newTestService(store, NoStats{}, NoRanking{})

	q := MapQuery{
		Box:        geo.BBox{MinLng: -67, MinLat: 10.4, MaxLng: -66.8, MaxLat: 10.75},
		Zoom:       12,
		Resolution: geo.ResolutionForZoom(12),
	}
	result, err := service.Map(context.Background(), q)
	if err != nil {
		t.Fatalf("Map: %v", err)
	}

	// §8 item 13: total is the places behind the clusters; counts sum to it when the
	// result is not truncated.
	if result.Total != len(clusterFixture) {
		t.Errorf("Total = %d, want %d", result.Total, len(clusterFixture))
	}
	if result.Truncated {
		t.Error("Truncated = true, want false")
	}
	sum := 0
	for _, c := range result.Clusters {
		sum += c.Count
		if c.Resolution != 7 {
			t.Errorf("cluster resolution = %d, want 7 for zoom 12", c.Resolution)
		}
	}
	if sum != result.Total {
		t.Errorf("cluster counts sum to %d, want total %d", sum, result.Total)
	}
}

func TestMapPropagatesTruncationAndFailure(t *testing.T) {
	store := &fakeStore{mapPts: clusterFixture[:1], truncated: true}
	service := newTestService(store, NoStats{}, NoRanking{})

	result, err := service.Map(context.Background(), MapQuery{Zoom: 12, Resolution: 7})
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	if !result.Truncated {
		t.Error("Truncated = false, want true from the store probe")
	}

	broken := newTestService(&fakeStore{mapErr: errors.New("boom")}, NoStats{}, NoRanking{})
	if _, err := broken.Map(context.Background(), MapQuery{Zoom: 12, Resolution: 7}); err == nil {
		t.Fatal("Map returned nil error, want the store failure wrapped")
	}
}

// The validated search filters travel from the service contract down to the
// store untouched: the store, not the service, owns how they become SQL.
func TestListPassesSearchFiltersToTheStore(t *testing.T) {
	store := &fakeStore{listRows: []ListRow{{ID: "p1", Title: "Alfa"}}, listTotal: 1}
	service := newTestService(store, &fakeStats{}, NoRanking{})

	maxPrice := 10.0
	openAt := time.Date(2026, 11, 8, 3, 30, 0, 0, time.UTC)
	q := nearQuery
	q.Q = "terraza"
	q.Dish = "pepiada"
	q.Attributes = []string{"terraza", "petfriendly"}
	q.MaxPrice = &maxPrice
	q.OpenAt = &openAt

	if _, err := service.List(context.Background(), q); err != nil {
		t.Fatalf("List: %v", err)
	}
	got := store.lastList
	if got.q != "terraza" || got.dish != "pepiada" {
		t.Errorf("q/dish = %q/%q", got.q, got.dish)
	}
	if len(got.attributes) != 2 || got.attributes[0] != "terraza" || got.attributes[1] != "petfriendly" {
		t.Errorf("attributes = %v", got.attributes)
	}
	if got.maxPrice == nil || *got.maxPrice != 10.0 {
		t.Errorf("maxPrice = %v", got.maxPrice)
	}
	if got.openAt == nil || !got.openAt.Equal(openAt) {
		t.Errorf("openAt = %v", got.openAt)
	}
}

func TestMapPassesSearchFiltersToTheStore(t *testing.T) {
	store := &fakeStore{}
	service := newTestService(store, &fakeStats{}, NoRanking{})

	maxPrice := 10.0
	openAt := time.Date(2026, 11, 8, 3, 30, 0, 0, time.UTC)
	_, err := service.Map(context.Background(), MapQuery{
		Box:        geo.BBox{MinLng: -66.99, MinLat: 10.41, MaxLng: -66.82, MaxLat: 10.54},
		Zoom:       12,
		Q:          "terraza",
		Dish:       "pepiada",
		Attributes: []string{"terraza"},
		MaxPrice:   &maxPrice,
		OpenAt:     &openAt,
	})
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	got := store.lastMap
	if got.q != "terraza" || got.dish != "pepiada" || len(got.attributes) != 1 || got.attributes[0] != "terraza" {
		t.Errorf("filters = %+v", got)
	}
	if got.maxPrice == nil || *got.maxPrice != 10.0 || got.openAt == nil || !got.openAt.Equal(openAt) {
		t.Errorf("maxPrice/openAt = %v/%v", got.maxPrice, got.openAt)
	}
}
