package places

import (
	"context"
	"errors"
	"strconv"
	"time"

	h3 "github.com/uber/h3-go/v4"

	"github.com/redis/go-redis/v9"
)

// Cache keys owned by the worker and read by this package (p1-discovery.md §5).
//
// Nothing here may contain the request coordinate, a user id, or anything derived from
// either: a cell is a coarse bucket, and a coordinate-keyed cache would build a trace of
// where users asked from (D1/D15/S18).
const (
	// TrendingTTL outlives one producer run, so a single failed batch degrades slowly
	// instead of blanking every zone at once.
	TrendingTTL = time.Hour
	// StatsTTL is longer: a rating aggregate does not change shape as fast as a rank.
	StatsTTL = 24 * time.Hour

	trendingPrefix = "melia:trending:z:"
	statsPrefix    = "melia:place:stats:"

	// TrendingPageCap bounds one ranking read; the list page is never larger than the
	// API's maximum page size.
	TrendingPageCap = 100
)

// TrendingKey is the zset of one H3 zone: member is a place id, score is its rank.
func TrendingKey(cell string) string { return trendingPrefix + cell }

// StatsKey is the hash of one place's published aggregate.
func StatsKey(placeID string) string { return statsPrefix + placeID }

// ParentZone returns the enclosing cell one resolution coarser, which is what a zone miss
// falls back to before the answer degrades to rating order (S28). ok is false when the
// cell cannot be parsed or already sits at the coarsest resolution.
func ParentZone(cell string) (parent string, ok bool) {
	index := h3.CellFromString(cell)
	if !index.IsValid() {
		return "", false
	}
	resolution := index.Resolution()
	if resolution == 0 {
		return "", false
	}
	parentIndex, err := index.Parent(resolution - 1)
	if err != nil {
		return "", false
	}
	return h3.CellToString(parentIndex), true
}

// redisReader is the narrow seam the readers share: it keeps the Redis command surface out
// of the tests, which run without a server (AGENTS.md).
type redisReader interface {
	ZRevRange(ctx context.Context, key string, start, stop int64) *redis.StringSliceCmd
	HMGet(ctx context.Context, key string, fields ...string) *redis.SliceCmd
}

// RedisRanking reads the worker-published zone rankings.
type RedisRanking struct {
	client redisReader
}

// NewRedisRanking wires the ranking reader.
func NewRedisRanking(client *redis.Client) *RedisRanking {
	return &RedisRanking{client: client}
}

// newRedisRankingWith allows the tests to pass the narrow interface.
func newRedisRankingWith(client redisReader) *RedisRanking { return &RedisRanking{client: client} }

// Trending returns the ranked place ids of one zone. ok=false means "this zone has no
// ranking", which is a degraded answer and never an error (S4/S28).
func (r *RedisRanking) Trending(ctx context.Context, zone string) ([]string, bool, error) {
	ids, err := r.client.ZRevRange(ctx, TrendingKey(zone), 0, TrendingPageCap-1).Result()
	if err != nil {
		return nil, false, err
	}
	if len(ids) == 0 {
		return nil, false, nil
	}
	return ids, true, nil
}

// RedisStats reads the worker-published rating aggregates.
type RedisStats struct {
	client redisReader
}

// NewRedisStats wires the aggregate reader.
func NewRedisStats(client *redis.Client) *RedisStats { return &RedisStats{client: client} }

// newRedisStatsWith allows the tests to pass the narrow interface.
func newRedisStatsWith(client redisReader) *RedisStats { return &RedisStats{client: client} }

// PlaceStats returns the aggregate of every place that has one published. A place without a
// hash is absent from the map, which the callers render as a null rating — never 0 (S26).
func (s *RedisStats) PlaceStats(ctx context.Context, ids []string) (map[string]Stats, error) {
	result := make(map[string]Stats, len(ids))
	var errs []error
	for _, id := range ids {
		values, err := s.client.HMGet(ctx, StatsKey(id), "ratingAvg", "reviewCount").Result()
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if len(values) != 2 {
			continue
		}
		average, okAverage := parseFloat(values[0])
		count, okCount := parseInt(values[1])
		if !okAverage || !okCount {
			continue
		}
		result[id] = Stats{Average: average, Count: count}
	}
	if len(errs) > 0 && len(result) == 0 {
		return nil, errors.Join(errs...)
	}
	return result, nil
}

// parseFloat reads a Redis value as a float. A missing hash yields nil, which is not a
// number and therefore not an aggregate.
func parseFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case string:
		parsed, err := strconv.ParseFloat(typed, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	case []byte:
		parsed, err := strconv.ParseFloat(string(typed), 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

// parseInt reads a Redis value as an integer count.
func parseInt(value any) (int, bool) {
	switch typed := value.(type) {
	case string:
		parsed, err := strconv.Atoi(typed)
		if err != nil {
			return 0, false
		}
		return parsed, true
	case []byte:
		parsed, err := strconv.Atoi(string(typed))
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}
