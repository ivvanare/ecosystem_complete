package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/geo"
	"github.com/ivvanare/ecosystem_complete/api/internal/places"
)

// fakeDiscovery is a scriptable PlaceDiscovery. With panicOnCall it proves that
// validation runs before the database: an invalid request never reaches the store
// (p1-discovery.md §3, S3).
type fakeDiscovery struct {
	listResult  places.ListResult
	listErr     error
	mapResult   places.MapResult
	mapErr      error
	panicOnCall bool
	lastList    places.ListQuery
	lastMap     places.MapQuery
}

func (f *fakeDiscovery) List(_ context.Context, q places.ListQuery) (places.ListResult, error) {
	if f.panicOnCall {
		panic("the read path must not be called for an invalid request")
	}
	f.lastList = q
	return f.listResult, f.listErr
}

func (f *fakeDiscovery) Map(_ context.Context, q places.MapQuery) (places.MapResult, error) {
	if f.panicOnCall {
		panic("the read path must not be called for an invalid request")
	}
	f.lastMap = q
	return f.mapResult, f.mapErr
}

func newPlacesHandler(discovery PlaceDiscovery) http.Handler {
	return New(Options{Discovery: discovery, Logger: slog.New(slog.DiscardHandler)})
}

func errorFields(t *testing.T, rec *httptest.ResponseRecorder) (string, []fieldError) {
	t.Helper()
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Fields []fieldError `json:"fields"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, rec.Body.String())
	}
	return body.Error.Code, body.Error.Details.Fields
}

// The validation matrix of p1-discovery.md §3 that belongs to this commit: every
// case is 422 validation_failed with the closed field vocabulary, and every case
// runs before the store (§8 items 1, 2, 3, 10, 11).
func TestPlacesValidationMatrix(t *testing.T) {
	cases := []struct {
		name  string
		query string
		field string
		code  string
	}{
		{"neither locator", "", "locator", "required"},
		{"both locators", "?near=10,-66&bbox=1,2,3,4", "bbox", "invalid"},
		{"latitude out of range", "?near=91,0", "near", "invalid"},
		{"near not a pair", "?near=abc", "near", "invalid"},
		{"near three parts", "?near=10,20,30", "near", "invalid"},
		{"longitude not numeric", "?near=10.5,abc", "near", "invalid"},
		{"radius zero", "?near=10,-66&radius=0", "radius", "invalid"},
		{"radius not integer", "?near=10,-66&radius=abc", "radius", "invalid"},
		{"radius over the maximum", "?near=10,-66&radius=50001", "radius", "too_long"},
		{"bbox three parts", "?bbox=1,2,3&zoom=12", "bbox", "invalid"},
		{"bbox five parts", "?bbox=1,2,3,4,5&zoom=12", "bbox", "invalid"},
		{"bbox inverted longitude", "?bbox=10,10,5,20&zoom=12", "bbox", "invalid"},
		{"bbox inverted latitude", "?bbox=-60,20,-50,10&zoom=12", "bbox", "invalid"},
		{"bbox anti-meridian", "?bbox=170,10,-170,20&zoom=12", "bbox", "invalid"},
		{"bbox over the area cap", "?bbox=-180,-85,180,85&zoom=12", "bbox", "too_long"},
		{"zoom missing in map mode", "?bbox=-67,10.4,-66.8,10.75", "zoom", "required"},
		{"zoom over the maximum", "?bbox=-67,10.4,-66.8,10.75&zoom=22", "zoom", "invalid"},
		{"zoom not an integer", "?bbox=-67,10.4,-66.8,10.75&zoom=abc", "zoom", "invalid"},
		{"country not alpha-2", "?near=10,-66&country=ven", "country", "invalid"},
		{"country not uppercase", "?near=10,-66&country=ve", "country", "invalid"},
		{"region not ISO 3166-2", "?near=10,-66&region=ve-a", "region", "invalid"},
		{"category not a uuid", "?near=10,-66&category=not-a-uuid", "category", "invalid"},
		{"sort outside the enum", "?near=10,-66&sort=popular", "sort", "invalid"},
		// Commit 3 (p1-discovery.md §3): the search and filter parameters, same
		// closed vocabulary, validated before any database call (S21).
		{"q too short", "?near=10,-66&q=a", "q", "too_short"},
		{"q too long", "?near=10,-66&q=" + strings.Repeat("a", 81), "q", "too_long"},
		{"dish too short", "?near=10,-66&dish=x", "dish", "too_short"},
		{"dish too long", "?near=10,-66&dish=" + strings.Repeat("a", 81), "dish", "too_long"},
		{"maxPrice not numeric", "?near=10,-66&maxPrice=abc", "maxPrice", "invalid"},
		{"maxPrice negative", "?near=10,-66&maxPrice=-1", "maxPrice", "invalid"},
		{"openAt not an instant", "?near=10,-66&openAt=not-a-date", "openAt", "invalid"},
		{"attributes unknown", "?near=10,-66&attributes=terraza,brcoli", "attributes", "unknown"},
		{"q too short in map mode", "?bbox=-67,10.4,-66.8,10.75&zoom=12&q=a", "q", "too_short"},
		{"attributes unknown in map mode", "?bbox=-67,10.4,-66.8,10.75&zoom=12&attributes=brcoli", "attributes", "unknown"},
		{"maxPrice invalid in map mode", "?bbox=-67,10.4,-66.8,10.75&zoom=12&maxPrice=-5", "maxPrice", "invalid"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := newPlacesHandler(&fakeDiscovery{panicOnCall: true})
			rec := do(t, handler, http.MethodGet, "/v1/places"+tc.query)

			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422 (%s)", rec.Code, rec.Body.String())
			}
			code, fields := errorFields(t, rec)
			if code != "validation_failed" {
				t.Errorf("error code = %q, want validation_failed", code)
			}
			if len(fields) == 0 {
				t.Fatalf("no field errors in %s", rec.Body.String())
			}
			if fields[0].Field != tc.field || fields[0].Code != tc.code {
				t.Errorf("fields[0] = %+v, want {%s %s}", fields[0], tc.field, tc.code)
			}
		})
	}
}

// listBody mirrors the documented list response (openapi.yaml PlaceListPage).
type listBody struct {
	Mode        string `json:"mode"`
	Page        int    `json:"page"`
	Limit       int    `json:"limit"`
	Total       int    `json:"total"`
	SortApplied string `json:"sortApplied"`
	Degraded    bool   `json:"degraded"`
	Data        []struct {
		CoPlace  string `json:"coPlace"`
		Name     string `json:"name"`
		Category struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"category"`
		Country        string  `json:"country"`
		Region         *string `json:"region"`
		Latitude       float64 `json:"latitude"`
		Longitude      float64 `json:"longitude"`
		Address        *string `json:"address"`
		DistanceMeters *int    `json:"distanceMeters"`
		Rating         *struct {
			Average float64 `json:"average"`
			Count   int     `json:"count"`
		} `json:"rating"`
		TrendingRank *int `json:"trendingRank"`
	} `json:"data"`
}

func decodeList(t *testing.T, rec *httptest.ResponseRecorder) listBody {
	t.Helper()
	var body listBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, rec.Body.String())
	}
	return body
}

// §8 item 4: a radius search with no results is 200 with an empty page, not an error.
func TestPlacesListEmptyIsNotAnError(t *testing.T) {
	discovery := &fakeDiscovery{listResult: places.ListResult{
		Rows: nil, Total: 0, Page: 1, Limit: 20, SortApplied: places.SortDistance,
	}}
	rec := do(t, newPlacesHandler(discovery), http.MethodGet, "/v1/places?near=10.4806,-66.9036")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	body := decodeList(t, rec)
	if body.Mode != "list" || body.Total != 0 || len(body.Data) != 0 {
		t.Errorf("body = %+v, want empty list page", body)
	}
	if body.SortApplied != "distance" || body.Degraded {
		t.Errorf("sortApplied=%q degraded=%v, want distance/false", body.SortApplied, body.Degraded)
	}
	if !strings.Contains(rec.Body.String(), `"data":[]`) {
		t.Errorf("data must serialize as an empty array, not null: %s", rec.Body.String())
	}

	// Defaults: radius 2000, page 1, limit 20, sort distance.
	q := discovery.lastList
	if q.Near.Lat != 10.4806 || q.Near.Lng != -66.9036 {
		t.Errorf("near = %+v", q.Near)
	}
	if q.Radius != 2000 || q.Page != 1 || q.Limit != 20 || q.Sort != places.SortDistance {
		t.Errorf("query = %+v, want defaults", q)
	}
}

// §8 items 5, 8, 9: distances arrive non-null and ascending with the default sort,
// rating is null (not 0) when no aggregate exists, and the legal name never leaks.
func TestPlacesListMapsRows(t *testing.T) {
	region := "VE-A"
	address := "Calle 1"
	discovery := &fakeDiscovery{listResult: places.ListResult{
		Rows: []places.ListRow{
			{
				ID: "p1", Title: "Arepera El Proof", CategoryID: "c1", CategoryName: "Arepera",
				Country: "VE", Region: &region, Latitude: 10.480594, Longitude: -66.903606,
				Address: &address, DistanceM: 120.4, Rating: &places.Rating{Average: 4.6, Count: 128},
			},
			{
				ID: "p2", Title: "Hotel Proof", CategoryID: "c2", CategoryName: "Hotel",
				Country: "VE", Latitude: 10.481, Longitude: -66.904,
				DistanceM: 940.2,
			},
		},
		Total: 2, Page: 1, Limit: 20, SortApplied: places.SortDistance,
	}}
	rec := do(t, newPlacesHandler(discovery), http.MethodGet, "/v1/places?near=10.4806,-66.9036")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeList(t, rec)
	if len(body.Data) != 2 {
		t.Fatalf("data = %d rows, want 2", len(body.Data))
	}

	first := body.Data[0]
	if first.CoPlace != "p1" || first.Name != "Arepera El Proof" {
		t.Errorf("first = %+v", first)
	}
	if first.Category.ID != "c1" || first.Category.Name != "Arepera" {
		t.Errorf("first.Category = %+v", first.Category)
	}
	if first.Region == nil || *first.Region != "VE-A" || first.Address == nil || *first.Address != "Calle 1" {
		t.Errorf("first nullables = %v/%v", first.Region, first.Address)
	}
	if first.DistanceMeters == nil {
		t.Fatal("distanceMeters is null in list mode: it is computed, never fabricated")
	}
	if *first.DistanceMeters != 120 {
		t.Errorf("first.distanceMeters = %d, want 120 (whole metres)", *first.DistanceMeters)
	}
	if first.Rating == nil || first.Rating.Average != 4.6 || first.Rating.Count != 128 {
		t.Errorf("first.Rating = %+v, want {4.6 128}", first.Rating)
	}

	second := body.Data[1]
	if second.DistanceMeters == nil || *second.DistanceMeters != 940 {
		t.Errorf("second.distanceMeters = %v, want 940", second.DistanceMeters)
	}
	if *first.DistanceMeters > *second.DistanceMeters {
		t.Errorf("distanceMeters not ascending: %d then %d", *first.DistanceMeters, *second.DistanceMeters)
	}
	if second.Rating != nil {
		t.Errorf("second.Rating = %+v, want null when the worker published nothing", second.Rating)
	}
	if !strings.Contains(rec.Body.String(), `"rating":null`) {
		t.Errorf("rating must serialize as null, not 0: %s", rec.Body.String())
	}

	// The legal name and the owner stay out of the public read path (S24 spirit).
	for _, leaked := range []string{"tx_name", "co_owner_user"} {
		if strings.Contains(rec.Body.String(), leaked) {
			t.Errorf("response leaks %q: %s", leaked, rec.Body.String())
		}
	}
}

// §8 item 7 (transport half): sortApplied and degraded echo the order that actually
// ran, so a client can tell a cold ranking from an unpopular place.
func TestPlacesListEchoesSortAppliedAndDegraded(t *testing.T) {
	discovery := &fakeDiscovery{listResult: places.ListResult{
		Rows:  []places.ListRow{{ID: "p1", Title: "Alfa"}},
		Total: 1, Page: 1, Limit: 20, SortApplied: places.SortRating, Degraded: true,
	}}
	rec := do(t, newPlacesHandler(discovery), http.MethodGet, "/v1/places?near=10,-66&sort=trending")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: a missing ranking is degraded, never an error", rec.Code)
	}
	body := decodeList(t, rec)
	if body.SortApplied != "rating" {
		t.Errorf("sortApplied = %q, want rating (the order that actually ran)", body.SortApplied)
	}
	if !body.Degraded {
		t.Error("degraded = false, want true")
	}
	if discovery.lastList.Sort != places.SortTrending {
		t.Errorf("requested sort = %q, want trending passed through", discovery.lastList.Sort)
	}
}

func TestPlacesListFiltersAndPaging(t *testing.T) {
	discovery := &fakeDiscovery{listResult: places.ListResult{SortApplied: places.SortRating, Page: 2, Limit: 50}}
	rec := do(t, newPlacesHandler(discovery), http.MethodGet,
		"/v1/places?near=10,-66&country=VE&region=VE-A&category=43c43189-8813-4f55-8636-f2aff3c37c5d&sort=rating&page=2&limit=50")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	q := discovery.lastList
	if q.Country != "VE" || q.Region != "VE-A" || q.Category != "43c43189-8813-4f55-8636-f2aff3c37c5d" {
		t.Errorf("filters = %+v", q)
	}
	if q.Sort != places.SortRating || q.Page != 2 || q.Limit != 50 {
		t.Errorf("query = %+v", q)
	}

	// The shared paging contract: page floors at 1, limit caps at 100.
	bounded := &fakeDiscovery{listResult: places.ListResult{SortApplied: places.SortDistance}}
	do(t, newPlacesHandler(bounded), http.MethodGet, "/v1/places?near=10,-66&page=0&limit=500")
	if bounded.lastList.Page != 1 || bounded.lastList.Limit != 100 {
		t.Errorf("page/limit = %d/%d, want 1/100", bounded.lastList.Page, bounded.lastList.Limit)
	}
}

// The commit-3 filters map onto the list query: q and dish are separate
// questions (S17), attributes collapse duplicates and empty entries (the SQL
// counts the intersection against the cardinality, so a repeated code must
// count once), maxPrice is a number and openAt an instant.
func TestPlacesListMapsSearchFilters(t *testing.T) {
	discovery := &fakeDiscovery{listResult: places.ListResult{SortApplied: places.SortDistance}}
	rec := do(t, newPlacesHandler(discovery), http.MethodGet,
		"/v1/places?near=10,-66&q=terraza&dish=pepiada&attributes=terraza,terraza,petfriendly,&maxPrice=10.5&openAt=2026-11-08T03:30:00Z")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	q := discovery.lastList
	if q.Q != "terraza" || q.Dish != "pepiada" {
		t.Errorf("q/dish = %q/%q", q.Q, q.Dish)
	}
	if len(q.Attributes) != 2 || q.Attributes[0] != "terraza" || q.Attributes[1] != "petfriendly" {
		t.Errorf("attributes = %v, want [terraza petfriendly] deduplicated", q.Attributes)
	}
	if q.MaxPrice == nil || *q.MaxPrice != 10.5 {
		t.Errorf("maxPrice = %v, want 10.5", q.MaxPrice)
	}
	want := time.Date(2026, 11, 8, 3, 30, 0, 0, time.UTC)
	if q.OpenAt == nil || !q.OpenAt.Equal(want) {
		t.Errorf("openAt = %v, want %v", q.OpenAt, want)
	}
}

// The same filters are valid in map mode (S21).
func TestPlacesMapMapsSearchFilters(t *testing.T) {
	discovery := &fakeDiscovery{mapResult: places.MapResult{}}
	rec := do(t, newPlacesHandler(discovery), http.MethodGet,
		"/v1/places?bbox=-67,10.4,-66.8,10.75&zoom=12&q=hotel&attributes=petfriendly&maxPrice=25&openAt=2026-11-08T03:30:00Z")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	q := discovery.lastMap
	if q.Q != "hotel" || len(q.Attributes) != 1 || q.Attributes[0] != "petfriendly" {
		t.Errorf("filters = %+v", q)
	}
	if q.MaxPrice == nil || *q.MaxPrice != 25 || q.OpenAt == nil {
		t.Errorf("maxPrice/openAt = %v/%v", q.MaxPrice, q.OpenAt)
	}
}

// A parameter that belongs to the other mode is ignored, like page/limit in map
// mode (p1-discovery.md §1): it is never validated against its own rules.
func TestPlacesListIgnoresMapParameters(t *testing.T) {
	discovery := &fakeDiscovery{listResult: places.ListResult{SortApplied: places.SortDistance}}
	rec := do(t, newPlacesHandler(discovery), http.MethodGet, "/v1/places?near=10,-66&zoom=99")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with zoom ignored in list mode (%s)", rec.Code, rec.Body.String())
	}
}

// mapBody mirrors the documented map response (openapi.yaml ClusterMap).
type mapBody struct {
	Mode       string `json:"mode"`
	Zoom       int    `json:"zoom"`
	Resolution int    `json:"resolution"`
	Total      int    `json:"total"`
	Truncated  bool   `json:"truncated"`
	Clusters   []struct {
		Cell       string `json:"cell"`
		Resolution int    `json:"resolution"`
		Count      int    `json:"count"`
		Center     struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		} `json:"center"`
		AverageRating *float64 `json:"averageRating"`
	} `json:"clusters"`
}

// §8 items 12 (transport half), 13 and 14: the zoom selects the resolution, total is
// the places behind the clusters, and a cluster carries no administrative naming.
func TestPlacesMapResponse(t *testing.T) {
	average := 4.3
	discovery := &fakeDiscovery{mapResult: places.MapResult{
		Clusters: []places.Cluster{
			{Cell: "872830828ffffff", Resolution: 7, Count: 37, CenterLat: 10.48, CenterLng: -66.9, AverageRating: &average},
			{Cell: "87283082bffffff", Resolution: 7, Count: 1, CenterLat: 10.5, CenterLng: -66.85},
		},
		Total: 38,
	}}
	rec := do(t, newPlacesHandler(discovery), http.MethodGet, "/v1/places?bbox=-66.99,10.41,-66.82,10.54&zoom=12")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	var body mapBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, rec.Body.String())
	}

	if body.Mode != "map" || body.Zoom != 12 || body.Resolution != 7 {
		t.Errorf("mode/zoom/resolution = %q/%d/%d", body.Mode, body.Zoom, body.Resolution)
	}
	if body.Total != 38 || body.Truncated {
		t.Errorf("total=%d truncated=%v", body.Total, body.Truncated)
	}
	if len(body.Clusters) != 2 {
		t.Fatalf("clusters = %d, want 2", len(body.Clusters))
	}
	sum := 0
	for _, c := range body.Clusters {
		sum += c.Count
	}
	if sum != body.Total {
		t.Errorf("cluster counts sum to %d, want total %d", sum, body.Total)
	}

	first := body.Clusters[0]
	if first.Cell != "872830828ffffff" || first.Count != 37 || first.Resolution != 7 {
		t.Errorf("first cluster = %+v", first)
	}
	if first.Center.Latitude != 10.48 || first.Center.Longitude != -66.9 {
		t.Errorf("first center = %+v", first.Center)
	}
	if first.AverageRating == nil || *first.AverageRating != 4.3 {
		t.Errorf("first averageRating = %v, want 4.3", first.AverageRating)
	}
	if body.Clusters[1].AverageRating != nil {
		t.Errorf("second averageRating = %v, want null when unknown, never 0", body.Clusters[1].AverageRating)
	}

	// A cluster is a zone (S20/D14): no name, no district, no administrative claim.
	for _, forbidden := range []string{`"name"`, `"district"`} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Errorf("map response carries administrative naming %s: %s", forbidden, rec.Body.String())
		}
	}

	// The bbox is longitude-first, and the zoom selected the resolution.
	q := discovery.lastMap
	if q.Box != (geo.BBox{MinLng: -66.99, MinLat: 10.41, MaxLng: -66.82, MaxLat: 10.54}) {
		t.Errorf("bbox = %+v", q.Box)
	}
	if q.Zoom != 12 || q.Resolution != 7 {
		t.Errorf("zoom/resolution = %d/%d, want 12/7", q.Zoom, q.Resolution)
	}
}

// §8 item 12: same data, different grouping — the zoom is an arithmetic argument.
func TestPlacesMapZoomSelectsTheResolution(t *testing.T) {
	discovery := &fakeDiscovery{mapResult: places.MapResult{}}
	rec := do(t, newPlacesHandler(discovery), http.MethodGet, "/v1/places?bbox=-66.99,10.41,-66.82,10.54&zoom=15")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if discovery.lastMap.Resolution != 8 {
		t.Errorf("resolution = %d, want 8 for zoom 15", discovery.lastMap.Resolution)
	}
}

func TestPlacesMapEmptyIsNotAnError(t *testing.T) {
	discovery := &fakeDiscovery{mapResult: places.MapResult{}}
	rec := do(t, newPlacesHandler(discovery), http.MethodGet, "/v1/places?bbox=-66.99,10.41,-66.82,10.54&zoom=12")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"clusters":[]`) {
		t.Errorf("clusters must serialize as an empty array, not null: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"total":0`) {
		t.Errorf("total must be 0: %s", rec.Body.String())
	}
}

func TestPlacesReadFailureIsInternalError(t *testing.T) {
	discovery := &fakeDiscovery{listErr: errors.New("store down")}
	rec := do(t, newPlacesHandler(discovery), http.MethodGet, "/v1/places?near=10,-66")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	code, _ := errorFields(t, rec)
	if code != "internal_error" {
		t.Errorf("error code = %q, want internal_error", code)
	}

	discovery = &fakeDiscovery{mapErr: errors.New("store down")}
	rec = do(t, newPlacesHandler(discovery), http.MethodGet, "/v1/places?bbox=-67,10.4,-66.8,10.75&zoom=12")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("map status = %d, want 500", rec.Code)
	}
}

func TestPlacesRouteRequiresAWiredReadPath(t *testing.T) {
	// Absent rather than accepted-and-failing when the read path is not wired, like
	// the broker-gated routes.
	rec := do(t, New(Options{Logger: slog.New(slog.DiscardHandler)}), http.MethodGet, "/v1/places?near=10,-66")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 when Discovery is not wired", rec.Code)
	}
}
