package places

import (
	"testing"

	h3 "github.com/uber/h3-go/v4"

	"github.com/ivvanare/ecosystem_complete/api/internal/geo"
)

// cellString is the fixture oracle: the H3 library decides which cell a point belongs
// to; the code under test decides grouping, counts, aggregation and serialization.
func cellString(t *testing.T, lat, lng float64, resolution int) string {
	t.Helper()
	cell, err := h3.LatLngToCell(h3.NewLatLng(lat, lng), resolution)
	if err != nil {
		t.Fatalf("LatLngToCell(%v, %v, %d): %v", lat, lng, resolution, err)
	}
	return h3.CellToString(cell)
}

// clusterFixture spreads five points over ~25 km so the grouping differs per
// resolution; p4 and p5 sit a few metres apart, so they share a cell at every
// resolution this slice uses.
var clusterFixture = []MapPoint{
	{ID: "p1", Latitude: 10.4806, Longitude: -66.9036},
	{ID: "p2", Latitude: 10.5200, Longitude: -66.9200},
	{ID: "p3", Latitude: 10.7000, Longitude: -66.8500},
	{ID: "p4", Latitude: 10.4807, Longitude: -66.9037},
	{ID: "p5", Latitude: 10.48071, Longitude: -66.90371},
}

func TestClusterGroupsByCell(t *testing.T) {
	const resolution = 7 // zoom 11–13

	clusters, err := BuildClusters(clusterFixture, resolution, nil)
	if err != nil {
		t.Fatalf("Cluster: %v", err)
	}

	// Every point lands in exactly one cluster, and the grouping matches the cells
	// the library assigns at this resolution.
	want := map[string]int{}
	for _, p := range clusterFixture {
		want[cellString(t, p.Latitude, p.Longitude, resolution)]++
	}
	if len(clusters) != len(want) {
		t.Fatalf("got %d clusters, want %d (cells %v)", len(clusters), len(want), want)
	}

	total := 0
	for _, c := range clusters {
		total += c.Count
		if c.Count != want[c.Cell] {
			t.Errorf("cluster %s count = %d, want %d", c.Cell, c.Count, want[c.Cell])
		}
		if c.Resolution != resolution {
			t.Errorf("cluster %s resolution = %d, want %d", c.Cell, c.Resolution, resolution)
		}
		// The center is the cell's center, not an average of points.
		cell, err := h3.LatLngToCell(h3.NewLatLng(c.CenterLat, c.CenterLng), resolution)
		if err != nil || h3.CellToString(cell) != c.Cell {
			t.Errorf("cluster %s center (%v, %v) does not resolve to its own cell", c.Cell, c.CenterLat, c.CenterLng)
		}
		// A cluster is a zone: no rating was published, so the aggregate is unknown —
		// nil, never 0.
		if c.AverageRating != nil {
			t.Errorf("cluster %s averageRating = %v, want nil when unknown", c.Cell, *c.AverageRating)
		}
	}
	if total != len(clusterFixture) {
		t.Errorf("cluster counts sum to %d, want %d (the rows the query returned)", total, len(clusterFixture))
	}
}

func TestClusterSameDataDifferentGroupingPerZoom(t *testing.T) {
	// §8 item 12: zoom 12 clusters at resolution 7, zoom 15 at resolution 8 — same
	// data, different grouping, as a pure function over a fixed row set.
	at7, err := BuildClusters(clusterFixture, 7, nil)
	if err != nil {
		t.Fatalf("Cluster res 7: %v", err)
	}
	at8, err := BuildClusters(clusterFixture, 8, nil)
	if err != nil {
		t.Fatalf("Cluster res 8: %v", err)
	}

	for _, c := range at7 {
		if c.Resolution != 7 {
			t.Errorf("zoom 12 cluster resolution = %d, want 7", c.Resolution)
		}
	}
	for _, c := range at8 {
		if c.Resolution != 8 {
			t.Errorf("zoom 15 cluster resolution = %d, want 8", c.Resolution)
		}
	}
	// The finer resolution never merges cells the coarser one split, and this fixture
	// strictly splits at least one cell.
	if len(at8) <= len(at7) {
		t.Errorf("res 8 produced %d clusters, res 7 produced %d: the fixture must split", len(at8), len(at7))
	}

	sum7, sum8 := 0, 0
	for _, c := range at7 {
		sum7 += c.Count
	}
	for _, c := range at8 {
		sum8 += c.Count
	}
	if sum7 != len(clusterFixture) || sum8 != len(clusterFixture) {
		t.Errorf("count sums = %d and %d, both want %d", sum7, sum8, len(clusterFixture))
	}
}

func TestClusterAverageRatingIsANeutralAggregate(t *testing.T) {
	// Two points in one shared cell: one with a published rating, one without. The
	// cluster average covers what exists; a cell with no ratings is nil, never 0.
	points := []MapPoint{
		{ID: "a", Latitude: 10.4807, Longitude: -66.9037},
		{ID: "b", Latitude: 10.48071, Longitude: -66.90371},
		{ID: "c", Latitude: 10.9, Longitude: -66.1},
	}
	stats := map[string]Stats{
		"a": {Average: 4.5, Count: 10},
		// b has no published aggregate.
		"c": {Average: 3.0, Count: 4},
	}

	clusters, err := BuildClusters(points, 7, stats)
	if err != nil {
		t.Fatalf("Cluster: %v", err)
	}

	byCell := map[string]Cluster{}
	for _, c := range clusters {
		byCell[c.Cell] = c
	}

	shared := byCell[cellString(t, 10.4807, -66.9037, 7)]
	if shared.AverageRating == nil || *shared.AverageRating != 4.5 {
		t.Errorf("shared cell averageRating = %v, want 4.5 (only the published one)", shared.AverageRating)
	}

	solo := byCell[cellString(t, 10.9, -66.1, 7)]
	if solo.AverageRating == nil || *solo.AverageRating != 3.0 {
		t.Errorf("solo cell averageRating = %v, want 3.0", solo.AverageRating)
	}
}

func TestClusterOrdersByCountThenCell(t *testing.T) {
	// Deterministic output: most significant cluster first, ties broken by cell so a
	// rerun never reshuffles the map for no reason.
	clusters, err := BuildClusters(clusterFixture, 7, nil)
	if err != nil {
		t.Fatalf("Cluster: %v", err)
	}
	for i := 1; i < len(clusters); i++ {
		prev, cur := clusters[i-1], clusters[i]
		if prev.Count < cur.Count {
			t.Errorf("clusters not ordered by count desc: %d then %d", prev.Count, cur.Count)
		}
		if prev.Count == cur.Count && prev.Cell > cur.Cell {
			t.Errorf("equal counts not ordered by cell: %s then %s", prev.Cell, cur.Cell)
		}
	}
}

func TestClusterEmptyInput(t *testing.T) {
	clusters, err := BuildClusters(nil, 7, nil)
	if err != nil {
		t.Fatalf("BuildClusters(nil): %v", err)
	}
	if len(clusters) != 0 {
		t.Errorf("Cluster(nil) returned %d clusters, want 0", len(clusters))
	}
}

func TestZoneCellRoundsTheCoordinate(t *testing.T) {
	// The zone key is the H3 cell of the request coordinate at the worker's
	// resolution — the raw coordinate is never key material (D1/D15).
	near := geo.Coordinate{Lat: 10.4806, Lng: -66.9036}
	zone, err := ZoneCell(near)
	if err != nil {
		t.Fatalf("ZoneCell: %v", err)
	}
	want := cellString(t, near.Lat, near.Lng, geo.TrendingZoneResolution)
	if zone != want {
		t.Errorf("ZoneCell = %s, want %s (res %d)", zone, want, geo.TrendingZoneResolution)
	}
}
