package placeadmin

import (
	"errors"
	"testing"
)

// The transport decides between 422 and 404 with errors.Is on these sentinels, never by reading
// text: every error of this package carries the package prefix, so a prefix test maps "place not
// found" onto a validation failure and answers 422 for a row the caller may not see.
func TestValidationFailuresCarryTheirSentinel(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{
			name: "day of week out of range",
			err:  validateSchedules([]Schedule{{DayOfWeek: 0, Open: "10:00", Close: "11:00"}}),
			want: ErrInvalidScheduleSet,
		},
		{
			name: "open and close at the same time",
			err:  validateSchedules([]Schedule{{DayOfWeek: 1, Open: "10:00", Close: "10:00"}}),
			want: ErrInvalidScheduleSet,
		},
		{
			name: "a menu without a name",
			err:  validateMenus([]Menu{{Items: []MenuItem{}}}),
			want: ErrInvalidMenuSet,
		},
		{
			name: "a negative price",
			err:  validateMenus([]Menu{{Name: "Cena", Items: []MenuItem{{Name: "Arepa", Price: -1, Currency: "USD"}}}}),
			want: ErrInvalidMenuSet,
		},
		{
			name: "a valid week and card",
			err:  validateSchedules([]Schedule{{DayOfWeek: 6, Open: "20:00", Close: "02:00"}}),
			want: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			switch {
			case test.want == nil && test.err != nil:
				t.Fatalf("unexpected error: %v", test.err)
			case test.want != nil && test.err == nil:
				t.Fatalf("expected an error wrapping %v, got nil", test.want)
			case test.want != nil && !errors.Is(test.err, test.want):
				t.Fatalf("error %v does not wrap %v", test.err, test.want)
			}
		})
	}
}

// The replace-the-set request has rules the database cannot express for it: at most one COVER
// (the partial unique index would answer with a constraint violation the client cannot read),
// no repeated row, and a position that is a position. They live in a pure function so they are
// provable without PostgreSQL.
func TestValidatePhotoPlacements(t *testing.T) {
	tests := []struct {
		name       string
		placements []PhotoPlacement
		wantErr    bool
	}{
		{
			name: "one cover and two galleries is a valid gallery",
			placements: []PhotoPlacement{
				{ID: "a", Type: PhotoCover, Order: 0},
				{ID: "b", Type: PhotoGallery, Order: 1},
				{ID: "c", Type: PhotoGallery, Order: 2},
			},
		},
		{
			name:       "an empty set is deliberate: it retires every photo",
			placements: nil,
		},
		{
			name: "two covers are refused before the index has to say it",
			placements: []PhotoPlacement{
				{ID: "a", Type: PhotoCover, Order: 0},
				{ID: "b", Type: PhotoCover, Order: 1},
			},
			wantErr: true,
		},
		{
			name: "a repeated photo is refused: one row cannot sit in two positions",
			placements: []PhotoPlacement{
				{ID: "a", Type: PhotoGallery, Order: 0},
				{ID: "a", Type: PhotoGallery, Order: 1},
			},
			wantErr: true,
		},
		{
			name:       "a negative position is refused",
			placements: []PhotoPlacement{{ID: "a", Type: PhotoGallery, Order: -1}},
			wantErr:    true,
		},
		{
			name:       "an unknown kind is refused",
			placements: []PhotoPlacement{{ID: "a", Type: "THUMBNAIL", Order: 0}},
			wantErr:    true,
		},
		{
			name:       "a missing photo id is refused",
			placements: []PhotoPlacement{{ID: "", Type: PhotoGallery, Order: 0}},
			wantErr:    true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validatePhotoPlacements(test.placements)
			if test.wantErr && err == nil {
				t.Fatal("expected a validation error, got nil")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if test.wantErr && !errors.Is(err, ErrInvalidPhotoSet) {
				t.Fatalf("error %v does not wrap ErrInvalidPhotoSet", err)
			}
		})
	}
}
