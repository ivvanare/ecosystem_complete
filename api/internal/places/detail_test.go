package places

import (
	"context"
	"errors"
	"testing"
)

// detailStore is the persistence seam with a canned answer: the suite never needs PostgreSQL.
type detailStore struct {
	detail Detail
	err    error
	calls  int
}

func (s *detailStore) Detail(context.Context, string) (Detail, error) {
	s.calls++
	return s.detail, s.err
}

// publishedStats answers one aggregate per id and records what was asked for.
type publishedStats struct {
	byID map[string]Stats
	err  error
	seen []string
}

func (s *publishedStats) PlaceStats(_ context.Context, ids []string) (map[string]Stats, error) {
	s.seen = append(s.seen, ids...)
	return s.byID, s.err
}

// A place with no photos, hours, menus or attributes is a real first day: the answer is empty
// arrays, not null, so a client renders without guarding every field.
func TestAnEmptyPlaceAnswersEmptyArrays(t *testing.T) {
	detail, err := NewDetailService(&detailStore{detail: Detail{ID: "p1"}}, nil).
		Detail(context.Background(), "p1")
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if detail.Photos == nil || detail.Schedules == nil || detail.Menus == nil || detail.Attributes == nil {
		t.Fatalf("detail = %+v, want empty slices", detail)
	}
}

// The rating is what the worker published, never an aggregate computed here.
func TestThePublishedRatingIsMergedIn(t *testing.T) {
	stats := &publishedStats{byID: map[string]Stats{"p1": {Average: 4.5, Count: 12}}}
	detail, err := NewDetailService(&detailStore{detail: Detail{ID: "p1"}}, stats).
		Detail(context.Background(), "p1")
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if detail.Rating == nil || detail.Rating.Average != 4.5 || detail.Rating.Count != 12 {
		t.Fatalf("rating = %+v", detail.Rating)
	}
	if len(stats.seen) != 1 || stats.seen[0] != "p1" {
		t.Fatalf("asked for %v, want the one place", stats.seen)
	}
}

// No published aggregate means unknown: nil, never a zero-star place.
func TestAnAbsentAggregateLeavesTheRatingNil(t *testing.T) {
	detail, err := NewDetailService(&detailStore{detail: Detail{ID: "p1"}},
		&publishedStats{byID: map[string]Stats{}}).Detail(context.Background(), "p1")
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if detail.Rating != nil {
		t.Fatalf("rating = %+v, want nil when nothing was published", detail.Rating)
	}
}

// A cold cache is not a failure: the place is still readable without stars.
func TestAFailingStatsReadDoesNotFailThePlace(t *testing.T) {
	detail, err := NewDetailService(&detailStore{detail: Detail{ID: "p1"}},
		&publishedStats{err: errors.New("redis down")}).Detail(context.Background(), "p1")
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if detail.Rating != nil {
		t.Fatalf("rating = %+v, want nil", detail.Rating)
	}
}

func TestAnInvisiblePlaceIsNotFound(t *testing.T) {
	_, err := NewDetailService(&detailStore{err: ErrNotFound}, nil).
		Detail(context.Background(), "p1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// A nil error means success. Wrapping it produced "%!w(<nil>)" once and turned every readable
// place into a 500 — the SQL path is not exercised by the fakes, so the helper is pinned here.
func TestWrappingANilErrorStaysNil(t *testing.T) {
	if err := wrapDetailError(nil); err != nil {
		t.Fatalf("wrapDetailError(nil) = %v, want nil", err)
	}
	if err := wrapDetailError(ErrNotFound); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrapDetailError(ErrNotFound) = %v, want ErrNotFound", err)
	}
}

// A place with no hours is not closed: the flag stays nil.
func TestAPlaceWithoutHoursHasAnUnknownOpenNow(t *testing.T) {
	detail, err := NewDetailService(&detailStore{detail: Detail{ID: "p1"}}, nil).
		Detail(context.Background(), "p1")
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if detail.OpenNow != nil {
		t.Fatalf("openNow = %v, want nil", *detail.OpenNow)
	}
}
