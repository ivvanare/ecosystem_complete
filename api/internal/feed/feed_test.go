package feed

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeStore struct {
	items []Item
	err   error
	seen  Query
	calls int
}

func (f *fakeStore) Page(_ context.Context, q Query) ([]Item, error) {
	f.calls++
	f.seen = q
	return f.items, f.err
}

func at(minute int) time.Time {
	return time.Date(2026, 11, 15, 12, minute, 0, 0, time.UTC)
}

// The cursor has to survive a round trip exactly: it is the only thing that keeps a reader from
// seeing the same row twice.
func TestTheCursorRoundTrips(t *testing.T) {
	original := Cursor{At: at(30), ID: "5148c527-a785-4887-acd4-fe18fdbf6202"}
	decoded, has, err := DecodeCursor(original.Encode())
	if err != nil {
		t.Fatalf("DecodeCursor: %v", err)
	}
	if !has {
		t.Fatal("a cursor was reported as absent")
	}
	if !decoded.At.Equal(original.At) || decoded.ID != original.ID {
		t.Fatalf("decoded = %+v, want %+v", decoded, original)
	}
}

func TestAnEmptyCursorIsTheFirstPage(t *testing.T) {
	_, has, err := DecodeCursor("   ")
	if err != nil || has {
		t.Fatalf("has = %v, err = %v", has, err)
	}
}

// A cursor that does not decode is a client bug, and it must not be papered over as a restart.
func TestAMalformedCursorIsRejected(t *testing.T) {
	for _, token := range []string{"not-base64!!", "Zm9v", "MjAyNi0xMS0xNVQxMjozMDowMFo="} {
		if _, _, err := DecodeCursor(token); !errors.Is(err, ErrInvalidCursor) {
			t.Errorf("token %q → err = %v, want ErrInvalidCursor", token, err)
		}
	}
}

// The service asks the store for one more row than it shows: that extra row is the probe that
// says whether a next page exists, so the answer never promises a page that turns out empty.
func TestAPageAsksForOneExtraRowAndOffersTheNextCursor(t *testing.T) {
	store := &fakeStore{items: []Item{
		{Kind: KindReview, At: at(9), CoReview: "r1"},
		{Kind: KindCheckin, At: at(8), CoCheckin: "c1"},
		{Kind: KindPlan, At: at(7), Plan: &PlanRef{ID: "p1"}},
	}}
	page, err := New(store).Page(context.Background(), "u1", "", 2)
	if err != nil {
		t.Fatalf("Page: %v", err)
	}

	if store.seen.Limit != 3 {
		t.Fatalf("limit asked = %d, want 3 (2 shown + 1 probe)", store.seen.Limit)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(page.Items))
	}
	if page.Next == "" {
		t.Fatal("nextCursor is empty with another page available")
	}
	// The cursor points at the last row SHOWN, not at the probe row.
	cursor, _, err := DecodeCursor(page.Next)
	if err != nil {
		t.Fatalf("DecodeCursor: %v", err)
	}
	if cursor.ID != "c1" || !cursor.At.Equal(at(8)) {
		t.Fatalf("cursor = %+v, want the last shown row", cursor)
	}
}

// The last page says so, and an empty feed is a real state.
func TestTheLastPageHasNoCursor(t *testing.T) {
	page, err := New(&fakeStore{items: []Item{{Kind: KindReview, At: at(1), CoReview: "r1"}}}).
		Page(context.Background(), "u1", "", 20)
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if page.Next != "" {
		t.Fatalf("nextCursor = %q, want none", page.Next)
	}
	if page.Items == nil {
		t.Fatal("items is nil, want an empty-capable slice")
	}
}

func TestTheCursorIsForwardedToTheStore(t *testing.T) {
	cursor := Cursor{At: at(5), ID: "r9"}
	store := &fakeStore{}
	if _, err := New(store).Page(context.Background(), "u1", cursor.Encode(), 10); err != nil {
		t.Fatalf("Page: %v", err)
	}
	if store.seen.Cursor == nil || store.seen.Cursor.ID != "r9" {
		t.Fatalf("store cursor = %+v", store.seen.Cursor)
	}
	if store.seen.Viewer != "u1" {
		t.Fatalf("viewer = %q", store.seen.Viewer)
	}
}

func TestTheLimitIsClamped(t *testing.T) {
	store := &fakeStore{}
	if _, err := New(store).Page(context.Background(), "u1", "", 500); err != nil {
		t.Fatalf("Page: %v", err)
	}
	if store.seen.Limit != MaxLimit+1 {
		t.Fatalf("limit = %d, want %d (clamped + probe)", store.seen.Limit, MaxLimit+1)
	}
}

func TestAFailingStoreIsAnError(t *testing.T) {
	_, err := New(&fakeStore{err: errors.New("db down")}).Page(context.Background(), "u1", "", 10)
	if err == nil {
		t.Fatal("a failing store was reported as an empty feed")
	}
}
