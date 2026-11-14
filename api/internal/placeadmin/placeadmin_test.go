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
