// Package feed answers "what have the people I follow been doing", which is the only thing that
// makes following someone mean more than a flag on a review.
//
// Three sources, one shape: reviews, check-ins and public plans. Everything here is already
// visible to the reader elsewhere — the feed is a shortcut, never a leak.
package feed

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Kinds. A closed set: the client switches on this value.
const (
	KindReview  = "review"
	KindCheckin = "checkin"
	KindPlan    = "plan"
)

// ErrInvalidCursor is a cursor that does not decode. It is a 422, never a silent restart at the
// first page: a reader who scrolls and gets the top again has been shown a bug as content.
var ErrInvalidCursor = errors.New("feed: invalid cursor")

// Limits.
const (
	DefaultLimit = 20
	MaxLimit     = 50
)

// Actor is who did the thing. Display-safe fields only, the same pair the review read publishes.
type Actor struct {
	ID   string
	Name *string
	// AvatarURL is nil when the person never uploaded one.
	AvatarURL *string
}

// PlaceRef is the place a review or a check-in points at.
type PlaceRef struct {
	ID    string
	Title *string
}

// PlanRef is the plan a plan event describes.
type PlanRef struct {
	ID          string
	Name        string
	Visibility  string
	ScheduledAt *time.Time
}

// Item is one row of the feed. Kind decides which payload is present.
type Item struct {
	Kind  string
	At    time.Time
	Actor Actor

	// Place is set for a review and for a check-in.
	Place *PlaceRef

	// Review only.
	CoReview string
	Rating   int
	Comment  *string

	// Check-in only.
	CoCheckin string

	// Plan only.
	Plan *PlanRef
}

// Page is one page of the feed.
type Page struct {
	Items []Item
	// Next is the cursor for the next page, or "" on the last one.
	Next string
}

// Cursor is the position of the last row already delivered.
type Cursor struct {
	At time.Time
	ID string
}

// Encode renders a cursor as the opaque token a client echoes back. Opaque on purpose: it lets
// the shape change later without a contract break.
func (c Cursor) Encode() string {
	raw := c.At.UTC().Format(time.RFC3339Nano) + "|" + c.ID
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeCursor parses what a client sent. An empty token means "first page".
func DecodeCursor(token string) (Cursor, bool, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Cursor{}, false, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return Cursor{}, false, ErrInvalidCursor
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 || parts[1] == "" {
		return Cursor{}, false, ErrInvalidCursor
	}
	at, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return Cursor{}, false, ErrInvalidCursor
	}
	return Cursor{At: at, ID: parts[1]}, true, nil
}

// Query is one page request, already validated.
type Query struct {
	Viewer string
	Limit  int
	Cursor *Cursor
}

// Store is the persistence seam. One call, because the three sources are merged by the database:
// merging in memory would need a full page from each source to stay correct.
type Store interface {
	Page(ctx context.Context, q Query) ([]Item, error)
}

// Service serves the feed.
type Service struct {
	store Store
}

// New wires the service.
func New(store Store) *Service { return &Service{store: store} }

// Page returns one page. The store returns at most limit+1 rows; the extra one is the probe that
// says whether another page exists, so the answer never promises a next page that is empty.
func (s *Service) Page(ctx context.Context, viewer, cursorToken string, limit int) (Page, error) {
	cursor, hasCursor, err := DecodeCursor(cursorToken)
	if err != nil {
		return Page{}, err
	}

	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}

	query := Query{Viewer: viewer, Limit: limit + 1}
	if hasCursor {
		query.Cursor = &cursor
	}

	items, err := s.store.Page(ctx, query)
	if err != nil {
		return Page{}, fmt.Errorf("feed: %w", err)
	}

	page := Page{Items: items}
	if page.Items == nil {
		page.Items = []Item{}
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		last := page.Items[len(page.Items)-1]
		page.Next = Cursor{At: last.At, ID: itemID(last)}.Encode()
	}
	return page, nil
}

// itemID is the tie-breaker of the ordering: whatever identifier the row carries.
func itemID(item Item) string {
	switch item.Kind {
	case KindReview:
		return item.CoReview
	case KindCheckin:
		return item.CoCheckin
	case KindPlan:
		if item.Plan != nil {
			return item.Plan.ID
		}
	}
	return ""
}
