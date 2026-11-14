// Package geo validates request geography at the application edge — coordinates,
// radii, bounding boxes and zoom levels — before any database work happens
// (spec.md S1/S2, p1-discovery.md §3). The database keeps shape guards; validity is
// decided here, and an out-of-range value is never queried.
package geo

import (
	"math"
	"strconv"
	"strings"
)

// Issue is one validation failure in the API's closed field vocabulary
// (api/API.md §10): Field is the parameter name, Code one of required|invalid|too_long.
type Issue struct {
	Field string
	Code  string
}

// Error lets an Issue travel as an error inside the packages that consume it.
func (i Issue) Error() string { return i.Field + ": " + i.Code }

// Coordinate is a validated latitude/longitude pair in degrees.
type Coordinate struct {
	Lat float64
	Lng float64
}

// Closed bounds of a valid coordinate (diagram_base.txt: tb_place CHECKs).
const (
	MinLatitude  = -90
	MaxLatitude  = 90
	MinLongitude = -180
	MaxLongitude = 180
)

// Radius bounds in metres (p1-discovery.md §1).
const (
	DefaultRadius = 2000
	MinRadius     = 1
	MaxRadius     = 50000
)

// parts splits a comma-separated parameter, trimming the surrounding space of every
// component so " 10.5 , -66.5 " is the same request as "10.5,-66.5".
func parts(raw string) []string {
	chunks := strings.Split(raw, ",")
	for i := range chunks {
		chunks[i] = strings.TrimSpace(chunks[i])
	}
	return chunks
}

// finite parses one numeric component. NaN and ±Inf parse successfully with
// strconv but are never a coordinate, so they are rejected here (S2).
func finite(s string) (float64, bool) {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

// ParseCoordinate validates "lat,long" — latitude first, unlike GeoJSON. Non-finite
// and out-of-range values are rejected here, never queried (S2).
func ParseCoordinate(field, raw string) (Coordinate, *Issue) {
	chunks := parts(raw)
	if len(chunks) != 2 {
		return Coordinate{}, &Issue{Field: field, Code: "invalid"}
	}
	lat, ok := finite(chunks[0])
	if !ok {
		return Coordinate{}, &Issue{Field: field, Code: "invalid"}
	}
	lng, ok := finite(chunks[1])
	if !ok {
		return Coordinate{}, &Issue{Field: field, Code: "invalid"}
	}
	if lat < MinLatitude || lat > MaxLatitude || lng < MinLongitude || lng > MaxLongitude {
		return Coordinate{}, &Issue{Field: field, Code: "invalid"}
	}
	return Coordinate{Lat: lat, Lng: lng}, nil
}

// ParseRadius validates the metres radius; an absent value defaults to DefaultRadius.
func ParseRadius(raw string) (int, *Issue) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultRadius, nil
	}
	metres, err := strconv.Atoi(raw)
	if err != nil {
		return 0, &Issue{Field: "radius", Code: "invalid"}
	}
	switch {
	case metres < MinRadius:
		return 0, &Issue{Field: "radius", Code: "invalid"}
	case metres > MaxRadius:
		return 0, &Issue{Field: "radius", Code: "too_long"}
	}
	return metres, nil
}

// BBox is a validated viewport, longitude-first like GeoJSON.
type BBox struct {
	MinLng float64
	MinLat float64
	MaxLng float64
	MaxLat float64
}

// MaxBBoxAreaKm2 caps the viewport so the map query stays honest: a frame that could
// exceed the row cap is rejected, never silently truncated (p1-discovery.md §2).
const MaxBBoxAreaKm2 = 100_000

// kmPerDegree is the length of one degree of latitude at the equator. The area below
// is the flat approximation, which is accurate enough to decide whether a viewport is
// within the cap and never claims survey precision.
const kmPerDegree = 111.32

// areaKm2 returns the approximate area of the frame, narrowing longitude by the
// cosine of the mid latitude — the same reason a degree of longitude is worth less
// the farther it is from the equator.
func (b BBox) areaKm2() float64 {
	heightKm := (b.MaxLat - b.MinLat) * kmPerDegree
	midLat := (b.MinLat + b.MaxLat) / 2
	widthKm := (b.MaxLng - b.MinLng) * kmPerDegree * math.Cos(midLat*math.Pi/180)
	if widthKm < 0 {
		widthKm = -widthKm
	}
	return heightKm * widthKm
}

// ParseBBox validates "minLon,minLat,maxLon,maxLat": four numeric parts in range,
// strictly increasing (which also rejects anti-meridian crossing), under the area cap.
func ParseBBox(raw string) (BBox, *Issue) {
	invalid := &Issue{Field: "bbox", Code: "invalid"}
	chunks := parts(raw)
	if len(chunks) != 4 {
		return BBox{}, invalid
	}
	values := make([]float64, 4)
	for i, chunk := range chunks {
		v, ok := finite(chunk)
		if !ok {
			return BBox{}, invalid
		}
		values[i] = v
	}
	box := BBox{MinLng: values[0], MinLat: values[1], MaxLng: values[2], MaxLat: values[3]}
	if box.MinLat < MinLatitude || box.MaxLat > MaxLatitude {
		return BBox{}, invalid
	}
	if box.MinLng < MinLongitude || box.MaxLng > MaxLongitude {
		return BBox{}, invalid
	}
	// Strictly increasing: rejects an empty frame, an inverted one, and a frame that
	// wraps the anti-meridian, which this phase does not support.
	if box.MinLng >= box.MaxLng || box.MinLat >= box.MaxLat {
		return BBox{}, invalid
	}
	if box.areaKm2() > MaxBBoxAreaKm2 {
		return BBox{}, &Issue{Field: "bbox", Code: "too_long"}
	}
	return box, nil
}

// Zoom bounds (map zoom levels, p1-discovery.md §1).
const (
	MinZoom = 0
	MaxZoom = 21
)

// ParseZoom validates the integer zoom.
func ParseZoom(raw string) (int, *Issue) {
	zoom, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || zoom < MinZoom || zoom > MaxZoom {
		return 0, &Issue{Field: "zoom", Code: "invalid"}
	}
	return zoom, nil
}

// TrendingZoneResolution is the H3 resolution the worker writes zone rankings at
// (p1-discovery.md §4): the read path rounds a coordinate to this cell and only the
// cell is used — raw coordinates are never cache material (D15/S18).
const TrendingZoneResolution = 7

// ResolutionForZoom maps a map zoom to the H3 resolution used to cluster
// (p1-discovery.md §4). One resolution is stored; zoom only changes an arithmetic
// argument, never a column per zoom level (S16/D13).
func ResolutionForZoom(zoom int) int {
	switch {
	case zoom <= 4:
		return 3
	case zoom <= 7:
		return 5
	case zoom <= 10:
		return 6
	case zoom <= 13:
		return 7
	case zoom <= 16:
		return 8
	default:
		return 9
	}
}
