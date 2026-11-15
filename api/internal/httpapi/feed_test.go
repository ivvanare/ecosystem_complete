package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/feed"
)

type fakeFeed struct {
	page   feed.Page
	err    error
	cursor string
	limit  int
	seen   string
}

func (f *fakeFeed) Page(_ context.Context, viewer, cursor string, limit int) (feed.Page, error) {
	f.seen, f.cursor, f.limit = viewer, cursor, limit
	return f.page, f.err
}

func feedRouter(reader FeedReader) http.Handler {
	return New(Options{
		Feed:        reader,
		TokenParser: authedParser("u1"),
		Logger:      slog.New(slog.DiscardHandler),
	})
}

// The feed is the read where the social graph applies, so it needs the caller: a stranger gets
// nothing, not an unpersonalized feed.
func TestTheFeedRequiresAuthentication(t *testing.T) {
	reader := &fakeFeed{}
	rec := do(t, feedRouter(reader), http.MethodGet, "/v1/feed")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if reader.seen != "" {
		t.Fatal("the feed was read without a caller")
	}
}

func TestTheFeedMapsTheThreeKinds(t *testing.T) {
	name := "Ana"
	title := "Arepera Chacao"
	scheduled := time.Date(2026, 11, 20, 20, 0, 0, 0, time.UTC)
	reader := &fakeFeed{page: feed.Page{
		Items: []feed.Item{
			{Kind: feed.KindReview, At: time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC),
				Actor: feed.Actor{ID: "u2", Name: &name}, CoReview: "r1", Rating: 4,
				Comment: ptr("Volví"), Place: &feed.PlaceRef{ID: "p1", Title: &title}},
			{Kind: feed.KindCheckin, At: time.Date(2026, 11, 15, 11, 0, 0, 0, time.UTC),
				Actor: feed.Actor{ID: "u2", Name: &name}, CoCheckin: "c1",
				Place: &feed.PlaceRef{ID: "p1", Title: &title}},
			{Kind: feed.KindPlan, At: time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC),
				Actor: feed.Actor{ID: "u2", Name: &name},
				Plan:  &feed.PlanRef{ID: "pl1", Name: "Cena", Visibility: "PUBLIC", ScheduledAt: &scheduled}},
		},
		Next: "next-token",
	}}

	rec := bearerDo(t, feedRouter(reader), http.MethodGet, "/v1/feed?limit=3", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`"kind":"review"`, `"coReview":"r1"`, `"rating":4`, `"comment":"Volví"`,
		`"kind":"checkin"`, `"coCheckin":"c1"`,
		`"kind":"plan"`, `"coRoute":"pl1"`, `"visibility":"PUBLIC"`,
		`"place":{"id":"p1","title":"Arepera Chacao"}`,
		`"nextCursor":"next-token"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body = %s\nwant it to contain %s", body, want)
		}
	}
	if reader.limit != 3 {
		t.Fatalf("limit = %d", reader.limit)
	}
	if reader.seen != "u1" {
		t.Fatalf("read for %q", reader.seen)
	}
}

// An empty feed is a real state for someone who follows nobody.
func TestAnEmptyFeedAnswersAnEmptyArrayAndNoCursor(t *testing.T) {
	rec := bearerDo(t, feedRouter(&fakeFeed{page: feed.Page{}}), http.MethodGet, "/v1/feed", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"data":[]`) || !strings.Contains(body, `"nextCursor":null`) {
		t.Fatalf("body = %s", body)
	}
}

// A cursor that does not decode is a 422: never a silent restart at the top, which a reader
// would experience as an infinite scroll.
func TestAMalformedCursorIs422(t *testing.T) {
	rec := bearerDo(t, feedRouter(&fakeFeed{err: feed.ErrInvalidCursor}),
		http.MethodGet, "/v1/feed?cursor=not-base64!!", "")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"field":"cursor","code":"invalid"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestAnInvalidLimitIs422(t *testing.T) {
	rec := bearerDo(t, feedRouter(&fakeFeed{}), http.MethodGet, "/v1/feed?limit=0", "")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
}

func TestAFailingFeedIsA500(t *testing.T) {
	rec := bearerDo(t, feedRouter(&fakeFeed{err: errors.New("db down")}), http.MethodGet, "/v1/feed", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func ptr[T any](value T) *T { return &value }
