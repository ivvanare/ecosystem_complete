// Package trending computes the discovery ranking and the rating aggregates the read path
// serves. It runs only in the worker: no request ever aggregates tb_review, which is the
// whole point of precomputing (S24, D3).
package trending

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"

	h3 "github.com/uber/h3-go/v4"

	"github.com/redis/go-redis/v9"

	"github.com/ivvanare/ecosystem_complete/api/internal/geo"
	"github.com/ivvanare/ecosystem_complete/api/internal/places"
)

// Weights of the trending score. They are code, not configuration: a ranking that can be
// retuned by an environment variable is a ranking nobody can reason about after a year.
//
// The shape is deliberate — recency outranks volume. A place with ten reviews this week
// beats one with a thousand from last year, because that is what the word means (S25).
const (
	weightReviews7d  = 2.0
	weightReviews30d = 0.5
	weightRating     = 0.2
)

// Signal is one place plus the review facts the score needs, as read from the database by
// the worker in a single aggregate query.
type Signal struct {
	PlaceID string
	// Latitude and Longitude are only used to bucket the place into a zone; they are
	// never written to a cache key (S27).
	Latitude   float64
	Longitude  float64
	Reviews7d  int
	Reviews30d int
	RatingAvg  float64
	RatingN    int
}

// Score returns the trending value of one place. Deterministic: the same Signal always
// yields the same number, so a rerun of the same batch produces the same order (S25).
func Score(s Signal) float64 {
	return weightReviews7d*float64(s.Reviews7d) +
		weightReviews30d*float64(s.Reviews30d) +
		weightRating*s.RatingAvg
}

// Ranked is one member of a zone ranking.
type Ranked struct {
	PlaceID string
	Score   float64
}

// Rank groups the signals by H3 zone and orders each zone by score descending, ties broken
// by place id so the order is reproducible (S25).
//
// The cell is computed here, at the resolution the read path rounds to, and only the cell
// leaves this function — the coordinate stops here.
func Rank(signals []Signal) (map[string][]Ranked, error) {
	zones := make(map[string][]Ranked)
	for _, signal := range signals {
		cell, err := h3.LatLngToCell(h3.NewLatLng(signal.Latitude, signal.Longitude), geo.TrendingZoneResolution)
		if err != nil {
			return nil, fmt.Errorf("placing %s in a zone: %w", signal.PlaceID, err)
		}
		key := h3.CellToString(cell)
		zones[key] = append(zones[key], Ranked{PlaceID: signal.PlaceID, Score: Score(signal)})
	}
	for key := range zones {
		entries := zones[key]
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].Score != entries[j].Score {
				return entries[i].Score > entries[j].Score
			}
			return entries[i].PlaceID < entries[j].PlaceID
		})
		zones[key] = entries
	}
	return zones, nil
}

// Publisher is the Redis surface the writer needs, narrowed so the tests run without a
// server (AGENTS.md).
type Publisher interface {
	Pipelined(ctx context.Context, fn func(redis.Pipeliner) error) ([]redis.Cmder, error)
}

// Writer publishes rankings and aggregates.
type Writer struct {
	client  Publisher
	logger  *slog.Logger
	metrics *Metrics
}

// Metrics counts what one batch did, so a job that silently publishes nothing is visible.
type Metrics struct {
	Zones   int
	Places  int
	Ratings int
}

// NewWriter wires the publisher.
func NewWriter(client Publisher, logger *slog.Logger) *Writer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Writer{client: client, logger: logger, metrics: &Metrics{}}
}

// LastMetrics reports what the previous Publish did.
func (w *Writer) LastMetrics() Metrics { return *w.metrics }

// Publish writes one zone ranking and every place aggregate.
//
// A zone is written whole in one pipeline — DEL, ZADD, EXPIRE — so a reader never observes
// half a ranking: the old one disappears and the new one appears in the same transaction
// from Redis' point of view (S27).
func (w *Writer) Publish(ctx context.Context, zones map[string][]Ranked, signals []Signal) error {
	metrics := Metrics{Zones: len(zones), Places: len(signals)}

	_, err := w.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for cell, entries := range zones {
			key := places.TrendingKey(cell)
			pipe.Del(ctx, key)
			members := make([]redis.Z, 0, len(entries))
			for _, entry := range entries {
				members = append(members, redis.Z{Score: entry.Score, Member: entry.PlaceID})
			}
			pipe.ZAdd(ctx, key, members...)
			pipe.Expire(ctx, key, places.TrendingTTL)
		}
		for _, signal := range signals {
			// A place with no reviews publishes no aggregate: the read path renders a
			// null rating rather than a fabricated zero (S26).
			if signal.RatingN == 0 {
				continue
			}
			key := places.StatsKey(signal.PlaceID)
			pipe.HSet(ctx, key,
				"ratingAvg", strconv.FormatFloat(signal.RatingAvg, 'f', -1, 64),
				"reviewCount", strconv.Itoa(signal.RatingN),
			)
			pipe.Expire(ctx, key, places.StatsTTL)
			metrics.Ratings++
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("publishing rankings: %w", err)
	}
	w.metrics = &metrics
	w.logger.Info("trending published",
		slog.Int("zones", metrics.Zones),
		slog.Int("places", metrics.Places),
		slog.Int("ratings", metrics.Ratings),
	)
	return nil
}

// Loader reads the signals of every live place.
type Loader interface {
	Signals(ctx context.Context) ([]Signal, error)
}

// Batch is one run of the producer: read the signals, rank per zone, publish.
type Batch struct {
	loader Loader
	writer *Writer
	logger *slog.Logger
}

// NewBatch wires the job.
func NewBatch(loader Loader, writer *Writer, logger *slog.Logger) *Batch {
	if logger == nil {
		logger = slog.Default()
	}
	return &Batch{loader: loader, writer: writer, logger: logger}
}

// Name and Run satisfy the worker scheduler's job contract.
func (b *Batch) Name() string { return "trending" }

// Run executes one batch. It is wired into the P0 scheduler, so a failure is logged by the
// scheduler and retried on the next tick rather than taking the worker down.
func (b *Batch) Run(ctx context.Context) error {
	signals, err := b.loader.Signals(ctx)
	if err != nil {
		return fmt.Errorf("loading signals: %w", err)
	}
	zones, err := Rank(signals)
	if err != nil {
		return err
	}
	return b.writer.Publish(ctx, zones, signals)
}
