package places

import (
	"sort"

	h3 "github.com/uber/h3-go/v4"

	"github.com/ivvanare/ecosystem_complete/api/internal/geo"
)

// MapPoint is one row the viewport query returned: only what clustering needs.
type MapPoint struct {
	ID        string
	Latitude  float64
	Longitude float64
}

// Cluster is one H3 cell of the map: counts and neutral aggregates, never an
// administrative name (S20/D14). Cell is the H3 index serialized as a hex string —
// JSON numbers lose precision above 2^53 and the index uses the full 64-bit range.
type Cluster struct {
	Cell       string
	Resolution int
	Count      int
	CenterLat  float64
	CenterLng  float64
	// AverageRating is the mean of the published place ratings in the cell. It is nil
	// when no place in the cell has an aggregate — never 0.
	AverageRating *float64
}

// accumulator is one cell while the grouping is being built.
type accumulator struct {
	count      int
	ratingSum  float64
	ratedCount int
}

// BuildClusters groups points by H3 cell at one resolution: each cell is computed from
// the point and derived to the target resolution by parent arithmetic — never one
// column per zoom (S16/D13). Clustering happens over the rows the query already
// returned, so counts are exact within the returned set and honest only up to the row
// cap (p1-discovery.md §2/§4).
//
// The output is ordered by count descending, ties broken by cell, so a rerun never
// reshuffles the map for no reason.
func BuildClusters(points []MapPoint, resolution int, stats map[string]Stats) ([]Cluster, error) {
	if len(points) == 0 {
		return nil, nil
	}

	cells := make(map[h3.Cell]*accumulator, len(points))
	for _, point := range points {
		cell, err := h3.LatLngToCell(h3.NewLatLng(point.Latitude, point.Longitude), resolution)
		if err != nil {
			return nil, err
		}
		entry, ok := cells[cell]
		if !ok {
			entry = &accumulator{}
			cells[cell] = entry
		}
		entry.count++
		// Only published aggregates count: a place with no stats must not pull the
		// cell average towards zero.
		if published, ok := stats[point.ID]; ok {
			entry.ratingSum += published.Average
			entry.ratedCount++
		}
	}

	clusters := make([]Cluster, 0, len(cells))
	for cell, entry := range cells {
		center, err := h3.CellToLatLng(cell)
		if err != nil {
			return nil, err
		}
		cluster := Cluster{
			Cell:       h3.CellToString(cell),
			Resolution: resolution,
			Count:      entry.count,
			CenterLat:  center.Lat,
			CenterLng:  center.Lng,
		}
		if entry.ratedCount > 0 {
			average := entry.ratingSum / float64(entry.ratedCount)
			cluster.AverageRating = &average
		}
		clusters = append(clusters, cluster)
	}

	sort.Slice(clusters, func(i, j int) bool {
		if clusters[i].Count != clusters[j].Count {
			return clusters[i].Count > clusters[j].Count
		}
		return clusters[i].Cell < clusters[j].Cell
	})
	return clusters, nil
}

// ZoneCell resolves a coordinate to the H3 cell the worker writes zone rankings at
// (p1-discovery.md §4/§5): the request coordinate is rounded to a cell and only the
// cell is used, so no cache key ever contains where a user asked from (D1/D15).
func ZoneCell(c geo.Coordinate) (string, error) {
	cell, err := h3.LatLngToCell(h3.NewLatLng(c.Lat, c.Lng), geo.TrendingZoneResolution)
	if err != nil {
		return "", err
	}
	return h3.CellToString(cell), nil
}
