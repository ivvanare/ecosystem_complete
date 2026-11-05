// Package places implements the discovery read path: radius and viewport queries over
// core.tb_place, worker-published rating/trending reads that never aggregate tb_review
// in a request, and in-memory H3 clustering (p1-discovery.md).
package places

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/geo"
)

// Sort values the contract accepts (p1-discovery.md §1). SortTrending ships in the
// enum but has no ranking producer yet: it resolves through RankingReader and, while
// every zone misses, degrades to SortRating with Degraded set (S4/S6).
const (
	SortDistance = "distance"
	SortTrending = "trending"
	SortRating   = "rating"
)

// ListQuery is a validated list-mode request.
type ListQuery struct {
	Near     geo.Coordinate
	Radius   int
	Country  string
	Region   string
	Category string
	// Q searches the place's commercial name; Dish searches menu items and
	// returns the places that carry a match — two different questions with
	// separate indexes, never one box (S17). Both arrive length-validated.
	Q    string
	Dish string
	// Attributes is the conjunctive filter (S18): a place must carry EVERY
	// code. The edge already deduplicated it and checked it against the
	// vocabulary.
	Attributes []string
	// MaxPrice is a USD ceiling (S22): a place matches with at least one
	// active USD-priced item on an active menu. There is no currency
	// parameter; items in other currencies never match, on purpose.
	MaxPrice *float64
	// OpenAt asks the schedule question at one instant, answered in the
	// place's own timezone including intervals that cross midnight (S19).
	OpenAt *time.Time
	Sort   string
	Page   int
	Limit  int
}

// attributeVocabulary mirrors the governed catalogue seeded by ledger file
// 18-place-attributes.sql (S15/S23): an attribute the catalogue does not know
// is rejected, never created on the fly. Until attribute CRUD exists the seed
// IS the catalogue and this set is its code mirror; when CRUD lands this
// becomes a catalogue read.
var attributeVocabulary = map[string]bool{
	"terraza":        true,
	"petfriendly":    true,
	"frente_playa":   true,
	"aire_libre":     true,
	"musica_en_vivo": true,
}

// KnownAttribute reports whether code is live governed vocabulary (S18).
func KnownAttribute(code string) bool { return attributeVocabulary[code] }

// Rating is the worker-published aggregate of one place. It is nil on a row when no
// aggregate exists: absence is a normal state, never an error (S12 spirit).
type Rating struct {
	Average float64
	Count   int
}

// ListRow is one place of a list page. It never carries the legal name (tx_name) or
// the owner (co_owner_user): those stay out of the public read path (S24 spirit).
type ListRow struct {
	ID           string
	Title        string
	CategoryID   string
	CategoryName string
	Country      string
	Region       *string
	Latitude     float64
	Longitude    float64
	Address      *string
	// DistanceM is the metres from the request point: computed in list mode, never
	// fabricated.
	DistanceM float64
	Rating    *Rating
	// TrendingRank is set only when a zone ranking ordered the row.
	TrendingRank *int
}

// ListResult is one page of list mode.
type ListResult struct {
	Rows  []ListRow
	Total int
	Page  int
	Limit int
	// SortApplied echoes the order that actually ran; Degraded marks a trending
	// request that fell back to rating because no zone ranking existed, so a client
	// can tell a cold ranking from an unpopular place.
	SortApplied string
	Degraded    bool
}

// MapQuery is a validated map-mode request. The search and filter
// parameters are valid in both modes (S21).
type MapQuery struct {
	Box        geo.BBox
	Zoom       int
	Resolution int
	Country    string
	Region     string
	Category   string
	Q          string
	Dish       string
	Attributes []string
	MaxPrice   *float64
	OpenAt     *time.Time
}

// MapResult is the clustered viewport.
type MapResult struct {
	Clusters []Cluster
	// Total is the number of places behind the clusters, not the number of clusters.
	Total int
	// Truncated marks a viewport that hit the row cap: cluster counts then describe
	// the sample, and a client must not present them as totals.
	Truncated bool
}

// Stats is the rating aggregate the worker publishes for one place
// (p1-discovery.md §5).
type Stats struct {
	Average float64
	Count   int
}

// StatsReader reads the worker-published rating aggregates. The Redis reader lands
// with the trending producer; until then NoStats is wired.
type StatsReader interface {
	PlaceStats(ctx context.Context, ids []string) (map[string]Stats, error)
}

// NoStats implements StatsReader reporting that no aggregate was ever published.
type NoStats struct{}

// PlaceStats reports no aggregate for any place.
func (NoStats) PlaceStats(context.Context, []string) (map[string]Stats, error) { return nil, nil }

// RankingReader reads the trending ranking of one H3 zone cell. ok=false means "no
// ranking": a degraded answer, never an error (S4). The ranked read path and the
// first real implementation land with the trending producer (tasks.md T1.17); until
// then NoRanking is wired.
type RankingReader interface {
	Trending(ctx context.Context, zoneCell string) (rankedIDs []string, ok bool, err error)
}

// NoRanking implements RankingReader for the slice before the producer exists: every
// zone misses, so sort=trending always degrades to rating (S6).
type NoRanking struct{}

// Trending reports no ranking for any zone.
func (NoRanking) Trending(context.Context, string) ([]string, bool, error) { return nil, false, nil }

// Store is the persistence seam. The Postgres implementation runs the spatial
// predicates on co_location so the GiST index serves them (S16); tests fake it,
// because the suite never needs PostgreSQL (AGENTS.md).
type Store interface {
	List(ctx context.Context, q listStoreQuery) ([]ListRow, int, error)
	MapPoints(ctx context.Context, q mapStoreQuery) (points []MapPoint, truncated bool, err error)
}

// Service orchestrates the read path: store queries, aggregate merging, ranking
// fallback and clustering.
type Service struct {
	store   Store
	stats   StatsReader
	ranking RankingReader
	logger  *slog.Logger
}

// NewService wires the discovery read path.
func NewService(store Store, stats StatsReader, ranking RankingReader, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{store: store, stats: stats, ranking: ranking, logger: logger}
}

// List runs list mode.
//
// The requested order is resolved here, before the store is reached: a trending request
// whose zone has no ranking — or whose ranking read fails — becomes a rating request and
// is reported as degraded, so the client can tell a cold cache from an unpopular place
// (S4/S6). A missing aggregate never fails discovery: the rating comes back nil.
func (s *Service) List(ctx context.Context, q ListQuery) (ListResult, error) {
	sort := q.Sort
	if sort == "" {
		sort = SortDistance
	}

	degraded := false
	var rankedIDs []string
	if sort == SortTrending {
		zone, err := ZoneCell(q.Near)
		if err != nil {
			s.logger.Warn("zone cell unavailable, trending degrades to rating", slog.String("error", err.Error()))
			sort, degraded = SortRating, true
		} else {
			ids, ok, err := s.ranking.Trending(ctx, zone)
			switch {
			case err != nil:
				s.logger.Warn("ranking read failed, trending degrades to rating", slog.String("error", err.Error()))
				sort, degraded = SortRating, true
			case !ok:
				sort, degraded = SortRating, true
			default:
				rankedIDs = ids
			}
		}
	}

	// A ranked read still needs a deterministic base order to page over; the ranking is
	// then applied to the page.
	storeSort := sort
	if sort == SortTrending {
		storeSort = SortRating
	}

	rows, total, err := s.store.List(ctx, listStoreQuery{
		near:       q.Near,
		radius:     q.Radius,
		country:    q.Country,
		region:     q.Region,
		category:   q.Category,
		q:          q.Q,
		dish:       q.Dish,
		attributes: q.Attributes,
		maxPrice:   q.MaxPrice,
		openAt:     q.OpenAt,
		sort:       storeSort,
		page:       q.Page,
		limit:      q.Limit,
	})
	if err != nil {
		return ListResult{}, err
	}

	s.applyStats(ctx, rows)

	if len(rankedIDs) > 0 {
		applyRanking(rows, rankedIDs)
	}

	return ListResult{
		Rows:        rows,
		Total:       total,
		Page:        q.Page,
		Limit:       q.Limit,
		SortApplied: sort,
		Degraded:    degraded,
	}, nil
}

// applyStats merges the worker-published aggregates into the page. The read asks only
// for the page's ids, and a failure leaves the ratings nil rather than failing
// discovery: absence is a normal state (S12 spirit).
func (s *Service) applyStats(ctx context.Context, rows []ListRow) {
	if len(rows) == 0 {
		return
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	stats, err := s.stats.PlaceStats(ctx, ids)
	if err != nil {
		s.logger.Warn("place aggregates unavailable, ratings stay null", slog.String("error", err.Error()))
		return
	}
	for i := range rows {
		if published, ok := stats[rows[i].ID]; ok {
			rows[i].Rating = &Rating{Average: published.Average, Count: published.Count}
		}
	}
}

// applyRanking orders the page by the zone ranking and records each row's rank. Rows
// the ranking does not mention keep their relative order after the ranked ones.
func applyRanking(rows []ListRow, rankedIDs []string) {
	position := make(map[string]int, len(rankedIDs))
	for i, id := range rankedIDs {
		position[id] = i + 1
	}
	sorted := make([]ListRow, 0, len(rows))
	ranked := make([]ListRow, 0, len(rows))
	unranked := make([]ListRow, 0, 1)
	for _, row := range rows {
		r := row
		if rank, ok := position[r.ID]; ok {
			r.TrendingRank = &rank
			ranked = append(ranked, r)
			continue
		}
		unranked = append(unranked, r)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		return *ranked[i].TrendingRank < *ranked[j].TrendingRank
	})
	sorted = append(sorted, ranked...)
	sorted = append(sorted, unranked...)
	copy(rows, sorted)
}

// Map runs map mode.
func (s *Service) Map(ctx context.Context, q MapQuery) (MapResult, error) {
	resolution := q.Resolution
	if resolution == 0 {
		resolution = geo.ResolutionForZoom(q.Zoom)
	}

	points, truncated, err := s.store.MapPoints(ctx, mapStoreQuery{
		box:        q.Box,
		country:    q.Country,
		region:     q.Region,
		category:   q.Category,
		q:          q.Q,
		dish:       q.Dish,
		attributes: q.Attributes,
		maxPrice:   q.MaxPrice,
		openAt:     q.OpenAt,
	})
	if err != nil {
		return MapResult{}, err
	}

	stats := map[string]Stats(nil)
	if len(points) > 0 {
		ids := make([]string, 0, len(points))
		for _, point := range points {
			ids = append(ids, point.ID)
		}
		if published, err := s.stats.PlaceStats(ctx, ids); err != nil {
			s.logger.Warn("place aggregates unavailable, cluster ratings stay null", slog.String("error", err.Error()))
		} else {
			stats = published
		}
	}

	clusters, err := BuildClusters(points, resolution, stats)
	if err != nil {
		return MapResult{}, err
	}

	return MapResult{
		Clusters: clusters,
		// Total is the places behind the clusters, not the number of clusters.
		Total:     len(points),
		Truncated: truncated,
	}, nil
}
