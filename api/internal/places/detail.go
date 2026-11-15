package places

import (
	"context"
	"errors"
	"fmt"
)

// ErrNotFound means the place is not visible: absent, deleted or not active. The caller
// answers all three the same way, so a response never confirms that a place exists.
var ErrNotFound = errors.New("places: not found")

// Detail is the public read of one place: what a client needs after tapping a result.
//
// It deliberately carries no legal name and no owner — those belong to the backoffice — and
// nothing personalized, so a cached answer cannot depend on who asked.
type Detail struct {
	ID          string
	Name        string
	Description *string
	Category    CategoryRef
	Country     string
	Region      *string
	Latitude    float64
	Longitude   float64
	Address     *string
	// SocialMedia is the free-form object the place published, e.g. {"instagram": "..."}.
	SocialMedia map[string]string
	// Timezone is the place's own zone. Hours are place-local, so a client rendering
	// "opens at 08:00" means 08:00 there, not on the device.
	Timezone string
	// OpenNow is nil when the place published no hours: unknown is not "closed".
	OpenNow    *bool
	Photos     []Photo
	Schedules  []ScheduleDay
	Menus      []Menu
	Attributes []AttributeRef
	// Rating is nil when no aggregate was published: unknown is not zero stars.
	Rating *Stats
}

// CategoryRef is the category a place belongs to.
type CategoryRef struct {
	ID   string
	Name string
}

// Photo is one image of the gallery, cover first.
type Photo struct {
	ID    string
	URL   string
	Type  string
	Order int
}

// ScheduleDay is one row of the weekly hours.
type ScheduleDay struct {
	// DayOfWeek is ISO: 1 = Monday, 7 = Sunday.
	DayOfWeek int
	Open      string
	Close     string
}

// Menu is one section of the menu with its items.
type Menu struct {
	Name        string
	Description *string
	Items       []MenuItem
}

// MenuItem is one dish. Price is in the currency's own units, not cents.
type MenuItem struct {
	Name        string
	Description *string
	Price       float64
	Currency    string
}

// AttributeRef is one governed attribute, by code and display name.
type AttributeRef struct {
	Code string
	Name string
}

// DetailStore is the persistence seam for the detail read. It runs the fixed statements the
// read needs — never one per row — and answers ErrNotFound for anything invisible.
type DetailStore interface {
	Detail(ctx context.Context, placeID string) (Detail, error)
}

// DetailService serves the public detail read.
type DetailService struct {
	store DetailStore
	stats StatsReader
}

// NewDetailService wires the detail read. A nil stats reader is not a crash: the rating is
// simply never published, which is a legitimate state (NoStats).
func NewDetailService(store DetailStore, stats StatsReader) *DetailService {
	return &DetailService{store: store, stats: stats}
}

// Detail returns the place, with the published rating merged in.
//
// The rating is not aggregated here or in SQL from tb_review: it is what the worker published,
// so a request never turns into a table scan over every review ever written.
func (s *DetailService) Detail(ctx context.Context, placeID string) (Detail, error) {
	detail, err := s.store.Detail(ctx, placeID)
	if err != nil {
		return Detail{}, err
	}

	// Empty is an array, never null: a client renders without guarding every field (S9).
	if detail.Photos == nil {
		detail.Photos = []Photo{}
	}
	if detail.Schedules == nil {
		detail.Schedules = []ScheduleDay{}
	}
	if detail.Menus == nil {
		detail.Menus = []Menu{}
	}
	if detail.Attributes == nil {
		detail.Attributes = []AttributeRef{}
	}

	if s.stats != nil {
		published, err := s.stats.PlaceStats(ctx, []string{detail.ID})
		if err != nil {
			// A missing aggregate never fails the read: the rating comes back nil, exactly
			// like discovery does with a cold cache.
			return detail, nil
		}
		if stats, ok := published[detail.ID]; ok {
			detail.Rating = &stats
		}
	}
	return detail, nil
}

// wrapDetailError keeps the store's ErrNotFound recognizable through the chain.
func wrapDetailError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	return fmt.Errorf("places: reading the detail: %w", err)
}
