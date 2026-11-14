package placeadmin

import "testing"

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
		})
	}
}
