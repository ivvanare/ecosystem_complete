package geo

import "testing"

// The validation matrix of p1-discovery.md §3, as pure functions: every rule runs
// before any database work.

func TestParseCoordinateAcceptsValidPairs(t *testing.T) {
	valid := []struct {
		raw      string
		lat, lng float64
	}{
		{"10.4806,-66.9036", 10.4806, -66.9036},
		{"-90,180", -90, 180}, // closed bounds
		{"90,-180", 90, -180}, // closed bounds
		{"0,0", 0, 0},
		{" 10.5 , -66.5 ", 10.5, -66.5}, // surrounding space is tolerated
	}
	for _, tc := range valid {
		got, issue := ParseCoordinate("near", tc.raw)
		if issue != nil {
			t.Errorf("ParseCoordinate(%q) rejected with %v, want valid", tc.raw, issue)
			continue
		}
		if got.Lat != tc.lat || got.Lng != tc.lng {
			t.Errorf("ParseCoordinate(%q) = %+v, want {%v %v}", tc.raw, got, tc.lat, tc.lng)
		}
	}
}

func TestParseCoordinateRejectsInvalidPairs(t *testing.T) {
	invalid := []string{
		"10.5",         // one part
		"10.5,-66.5,1", // three parts
		"abc,-66.5",    // non-numeric latitude
		"10.5,abc",     // non-numeric longitude
		"91,0",         // latitude out of range
		"-91,0",        // latitude out of range
		"0,181",        // longitude out of range
		"0,-181",       // longitude out of range
		"NaN,0",        // not finite
		"Inf,0",        // not finite
		"0,-Inf",       // not finite
	}
	for _, raw := range invalid {
		got, issue := ParseCoordinate("near", raw)
		if issue == nil {
			t.Errorf("ParseCoordinate(%q) = %+v, want invalid", raw, got)
			continue
		}
		if issue.Field != "near" || issue.Code != "invalid" {
			t.Errorf("ParseCoordinate(%q) issue = %+v, want {near invalid}", raw, issue)
		}
	}
}

func TestParseRadius(t *testing.T) {
	t.Run("absent defaults to 2000", func(t *testing.T) {
		got, issue := ParseRadius("")
		if issue != nil || got != DefaultRadius {
			t.Errorf("ParseRadius(\"\") = %d, %v, want %d, nil", got, issue, DefaultRadius)
		}
	})

	valid := map[string]int{"1": 1, "2000": 2000, "50000": 50000}
	for raw, want := range valid {
		got, issue := ParseRadius(raw)
		if issue != nil || got != want {
			t.Errorf("ParseRadius(%q) = %d, %v, want %d, nil", raw, got, issue, want)
		}
	}

	t.Run("not an integer", func(t *testing.T) {
		for _, raw := range []string{"abc", "1.5", "20 00"} {
			if got, issue := ParseRadius(raw); issue == nil || issue.Code != "invalid" {
				t.Errorf("ParseRadius(%q) = %d, %v, want invalid", raw, got, issue)
			}
		}
	})

	t.Run("below the minimum", func(t *testing.T) {
		for _, raw := range []string{"0", "-5"} {
			if got, issue := ParseRadius(raw); issue == nil || issue.Code != "invalid" {
				t.Errorf("ParseRadius(%q) = %d, %v, want invalid", raw, got, issue)
			}
		}
	})

	t.Run("above the maximum is too_long", func(t *testing.T) {
		if got, issue := ParseRadius("50001"); issue == nil || issue.Code != "too_long" {
			t.Errorf("ParseRadius(50001) = %d, %v, want too_long", got, issue)
		}
	})
}

func TestParseBBox(t *testing.T) {
	t.Run("valid frames", func(t *testing.T) {
		valid := []struct{ raw string }{
			{"-66.99,10.41,-66.82,10.54"},
			{"-180,-90,-179,-89"}, // components at the closed bounds, small frame
			{"0,0,2.8,2.8"},       // ~97 000 km², just under the area cap
		}
		for _, tc := range valid {
			if got, issue := ParseBBox(tc.raw); issue != nil {
				t.Errorf("ParseBBox(%q) rejected with %v, want valid (%+v)", tc.raw, issue, got)
			}
		}
	})

	t.Run("malformed", func(t *testing.T) {
		invalid := []string{
			"1,2,3",          // three parts
			"1,2,3,4,5",      // five parts
			"a,b,c,d",        // non-numeric
			"10,10,5,20",     // minLon >= maxLon
			"-60,20,-50,10",  // minLat >= maxLat
			"170,10,-170,20", // anti-meridian crossing is out of scope
			"0,91,10,92",     // latitude out of range
			"181,0,182,10",   // longitude out of range
		}
		for _, raw := range invalid {
			got, issue := ParseBBox(raw)
			if issue == nil || issue.Field != "bbox" || issue.Code != "invalid" {
				t.Errorf("ParseBBox(%q) = %+v, %v, want {bbox invalid}", raw, got, issue)
			}
		}
	})

	t.Run("over the area cap is too_long", func(t *testing.T) {
		for _, raw := range []string{"-180,-85,180,85", "0,0,3,3" /* ~111 000 km² */} {
			got, issue := ParseBBox(raw)
			if issue == nil || issue.Field != "bbox" || issue.Code != "too_long" {
				t.Errorf("ParseBBox(%q) = %+v, %v, want {bbox too_long}", raw, got, issue)
			}
		}
	})
}

func TestParseZoom(t *testing.T) {
	valid := map[string]int{"0": 0, "12": 12, "21": 21}
	for raw, want := range valid {
		if got, issue := ParseZoom(raw); issue != nil || got != want {
			t.Errorf("ParseZoom(%q) = %d, %v, want %d, nil", raw, got, issue, want)
		}
	}

	for _, raw := range []string{"-1", "22", "abc", "1.5"} {
		if got, issue := ParseZoom(raw); issue == nil || issue.Code != "invalid" {
			t.Errorf("ParseZoom(%q) = %d, %v, want invalid", raw, got, issue)
		}
	}
}

func TestResolutionForZoom(t *testing.T) {
	// The table of p1-discovery.md §4: zoom changes an arithmetic argument, never a
	// stored column.
	cases := map[int]int{
		0: 3, 4: 3,
		5: 5, 7: 5,
		8: 6, 10: 6,
		11: 7, 13: 7,
		14: 8, 16: 8,
		17: 9, 21: 9,
	}
	for zoom, want := range cases {
		if got := ResolutionForZoom(zoom); got != want {
			t.Errorf("ResolutionForZoom(%d) = %d, want %d", zoom, got, want)
		}
	}
}
