package trending

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// The properties of S25, which are what make the word "trending" mean something.

func TestScoreRecencyDominatesVolume(t *testing.T) {
	// Ten reviews this week must outrank a hundred from last year: the old place is
	// outside both windows, so only its rating contributes.
	active := Signal{PlaceID: "active", Reviews7d: 10, Reviews30d: 10, RatingAvg: 4.0, RatingN: 10}
	popularLastYear := Signal{PlaceID: "old", Reviews7d: 0, Reviews30d: 0, RatingAvg: 5.0, RatingN: 1000}

	if Score(active) <= Score(popularLastYear) {
		t.Fatalf("score(active)=%v must beat score(popularLastYear)=%v", Score(active), Score(popularLastYear))
	}
}

func TestScoreWithoutReviewsIsRatingOnlyAndNeverOutranksActivity(t *testing.T) {
	silent := Signal{PlaceID: "silent", RatingAvg: 5.0, RatingN: 3}
	active := Signal{PlaceID: "active", Reviews7d: 1, Reviews30d: 1, RatingAvg: 3.0, RatingN: 1}

	if Score(silent) >= Score(active) {
		t.Fatalf("a place with no recent reviews scored %v, must stay under an active place's %v",
			Score(silent), Score(active))
	}
	// No reviews at all and no rating is zero, not an error and not a NaN.
	if got := Score(Signal{PlaceID: "empty"}); got != 0 {
		t.Fatalf("empty signal scored %v, want 0", got)
	}
}

func TestScoreIsDeterministic(t *testing.T) {
	signal := Signal{PlaceID: "p", Reviews7d: 3, Reviews30d: 7, RatingAvg: 4.25, RatingN: 7}
	for i := 0; i < 5; i++ {
		if Score(signal) != Score(signal) {
			t.Fatal("Score is not deterministic for the same input")
		}
	}
}

func TestRankGroupsByZoneAndOrdersDeterministically(t *testing.T) {
	// Two places in the same Caracas cell, one far away, one exact tie to exercise the
	// tie-break by place id.
	signals := []Signal{
		{PlaceID: "b", Latitude: 10.4806, Longitude: -66.9036, Reviews7d: 1},
		{PlaceID: "a", Latitude: 10.4807, Longitude: -66.9037, Reviews7d: 1},
		{PlaceID: "z", Latitude: 10.4808, Longitude: -66.9035, Reviews7d: 5},
		{PlaceID: "far", Latitude: -34.6037, Longitude: -58.3816, Reviews7d: 2},
	}

	zones, err := Rank(signals)
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	if len(zones) != 2 {
		t.Fatalf("got %d zones, want 2", len(zones))
	}

	var caracas []Ranked
	for cell, entries := range zones {
		if len(entries) == 3 {
			caracas = entries
			_ = cell
		}
	}
	if len(caracas) != 3 {
		t.Fatalf("no zone carries the three Caracas places: %+v", zones)
	}
	order := []string{caracas[0].PlaceID, caracas[1].PlaceID, caracas[2].PlaceID}
	want := []string{"z", "a", "b"} // highest score first, then the tie broken by id
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("zone order = %v, want %v", order, want)
		}
	}

	// A rerun over the same signals produces the same order.
	again, err := Rank(signals)
	if err != nil {
		t.Fatalf("Rank rerun: %v", err)
	}
	for cell, entries := range zones {
		for i := range entries {
			if again[cell][i].PlaceID != entries[i].PlaceID {
				t.Fatalf("zone %s reshuffled between identical runs", cell)
			}
		}
	}
}

// fakePublisher records the pipeline commands so the write shape can be asserted without a
// server (AGENTS.md).
type fakePublisher struct {
	commands []string
	err      error
}

func (f *fakePublisher) Pipelined(_ context.Context, fn func(redis.Pipeliner) error) ([]redis.Cmder, error) {
	if f.err != nil {
		return nil, f.err
	}
	pipe := &recordingPipeliner{record: &f.commands}
	return nil, fn(pipe)
}

// recordingPipeliner implements only what the writer calls; embedding redis.Pipeliner keeps
// the unused surface from having to be written out.
type recordingPipeliner struct {
	redis.Pipeliner
	record *[]string
}

func (p *recordingPipeliner) Del(_ context.Context, keys ...string) *redis.IntCmd {
	*p.record = append(*p.record, "DEL")
	return &redis.IntCmd{}
}

func (p *recordingPipeliner) ZAdd(_ context.Context, key string, members ...redis.Z) *redis.IntCmd {
	*p.record = append(*p.record, "ZADD:"+key)
	return &redis.IntCmd{}
}

func (p *recordingPipeliner) HSet(_ context.Context, key string, values ...any) *redis.IntCmd {
	*p.record = append(*p.record, "HSET:"+key)
	return &redis.IntCmd{}
}

func (p *recordingPipeliner) Expire(_ context.Context, key string, _ time.Duration) *redis.BoolCmd {
	*p.record = append(*p.record, "EXPIRE:"+key)
	return &redis.BoolCmd{}
}

func TestPublishWritesEachZoneWholeAndOnlyForRatedPlaces(t *testing.T) {
	publisher := &fakePublisher{}
	writer := NewWriter(publisher, discard())

	zones := map[string][]Ranked{
		"872830828ffffff": {{PlaceID: "p1", Score: 5}, {PlaceID: "p2", Score: 1}},
	}
	signals := []Signal{
		{PlaceID: "p1", RatingAvg: 4.5, RatingN: 10},
		{PlaceID: "p2"}, // no reviews: no aggregate is published
	}

	if err := writer.Publish(context.Background(), zones, signals); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// The zone is written whole: the old ranking is removed before the new one is added,
	// in the same pipeline.
	var sawDel, sawZAdd, sawZoneExpire bool
	for _, command := range publisher.commands {
		switch command {
		case "DEL":
			sawDel = true
		case "ZADD:melia:trending:z:872830828ffffff":
			sawZAdd = true
		case "EXPIRE:melia:trending:z:872830828ffffff":
			sawZoneExpire = true
		}
	}
	if !sawDel || !sawZAdd || !sawZoneExpire {
		t.Fatalf("zone not written whole: %v", publisher.commands)
	}

	// Only the rated place gets a hash, and no key carries a coordinate or a user.
	var stats float64
	for _, command := range publisher.commands {
		if len(command) > 5 && command[:5] == "HSET:" {
			stats++
			if command != "HSET:melia:place:stats:p1" {
				t.Fatalf("unexpected stats key %q", command)
			}
		}
	}
	if stats != 1 {
		t.Fatalf("published %v aggregates, want exactly the rated place", stats)
	}

	metrics := writer.LastMetrics()
	if metrics.Zones != 1 || metrics.Places != 2 || metrics.Ratings != 1 {
		t.Fatalf("metrics = %+v, want 1 zone / 2 places / 1 rating", metrics)
	}
}

func TestPublishReportsFailure(t *testing.T) {
	writer := NewWriter(&fakePublisher{err: errors.New("redis down")}, discard())
	if err := writer.Publish(context.Background(), nil, nil); err == nil {
		t.Fatal("Publish returned nil error with a failing pipeline, want it wrapped")
	}
}

// fakeLoader is the batch's read side.
type fakeLoader struct {
	signals []Signal
	err     error
}

func (f *fakeLoader) Signals(context.Context) ([]Signal, error) { return f.signals, f.err }

func TestBatchRanksAndPublishes(t *testing.T) {
	publisher := &fakePublisher{}
	batch := NewBatch(
		&fakeLoader{signals: []Signal{{PlaceID: "p1", Latitude: 10.48, Longitude: -66.9, Reviews7d: 2}}},
		NewWriter(publisher, discard()),
		discard(),
	)
	if batch.Name() != "trending" {
		t.Fatalf("job name = %q, want trending", batch.Name())
	}
	if err := batch.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(publisher.commands) == 0 {
		t.Fatal("Run published nothing")
	}
}

func TestBatchPropagatesLoaderFailure(t *testing.T) {
	batch := NewBatch(&fakeLoader{err: errors.New("db down")}, NewWriter(&fakePublisher{}, discard()), discard())
	if err := batch.Run(context.Background()); err == nil {
		t.Fatal("Run returned nil error with a failing loader, want it wrapped")
	}
}
