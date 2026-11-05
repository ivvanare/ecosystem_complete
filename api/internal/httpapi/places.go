package httpapi

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ivvanare/ecosystem_complete/api/internal/geo"
	"github.com/ivvanare/ecosystem_complete/api/internal/places"
)

// PlaceDiscovery is the discovery read surface (p1-discovery.md §1): list mode by
// radius, map mode by viewport. Public and read-only (S14).
type PlaceDiscovery interface {
	List(ctx context.Context, q places.ListQuery) (places.ListResult, error)
	Map(ctx context.Context, q places.MapQuery) (places.MapResult, error)
}

type placesHandlers struct {
	discovery PlaceDiscovery
	logger    *slog.Logger
}

// The shape guards of p1-discovery.md §3, applied at the edge: the database keeps its
// CHECKs, but a bad value is rejected here, before any query runs.
var (
	countryPattern = regexp.MustCompile(`^[A-Z]{2}$`)
	regionPattern  = regexp.MustCompile(`^[A-Z]{2}-[A-Z0-9]{1,3}$`)
	// uuidPattern is the uuid shape guard, shared by the category filter and the
	// review path parameters.
	uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// sortValues is the closed enum the contract accepts.
var sortValues = map[string]bool{
	places.SortDistance: true,
	places.SortTrending: true,
	places.SortRating:   true,
}

// Shared paging contract: page floors at 1, limit caps at 100 (openapi.yaml parameters).
func pageAndLimit(r *http.Request) (int, int) {
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 1 {
		page = 1
	}
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return page, limit
}

// discoveryFilters are the filters both modes share, already validated against
// the matrix of p1-discovery.md §3: the search and filter parameters of §12
// are valid in list mode and in map mode alike (S21).
type discoveryFilters struct {
	country    string
	region     string
	category   string
	q          string
	dish       string
	attributes []string
	maxPrice   *float64
	openAt     *time.Time
}

// filters validates the filters both modes share.
func filters(r *http.Request) (discoveryFilters, []fieldError) {
	var f discoveryFilters
	var fields []fieldError
	query := r.URL.Query()

	f.country = strings.TrimSpace(query.Get("country"))
	if f.country != "" && !countryPattern.MatchString(f.country) {
		fields = append(fields, fieldError{Field: "country", Code: "invalid"})
	}
	f.region = strings.TrimSpace(query.Get("region"))
	if f.region != "" && !regionPattern.MatchString(f.region) {
		fields = append(fields, fieldError{Field: "region", Code: "invalid"})
	}
	f.category = strings.TrimSpace(query.Get("category"))
	if f.category != "" && !uuidPattern.MatchString(f.category) {
		fields = append(fields, fieldError{Field: "category", Code: "invalid"})
	}

	f.q = searchTerm(query.Get("q"), "q", &fields)
	f.dish = searchTerm(query.Get("dish"), "dish", &fields)
	f.attributes = attributeFilter(query.Get("attributes"), &fields)

	// maxPrice is a USD amount (S22): non-numeric or negative is invalid. NaN
	// and infinities parse as numbers, so they are rejected explicitly.
	if raw := strings.TrimSpace(query.Get("maxPrice")); raw != "" {
		price, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(price) || math.IsInf(price, 0) || price < 0 {
			fields = append(fields, fieldError{Field: "maxPrice", Code: "invalid"})
		} else {
			f.maxPrice = &price
		}
	}

	// openAt is an ISO-8601 instant (S19); RFC 3339 is that profile.
	if raw := strings.TrimSpace(query.Get("openAt")); raw != "" {
		instant, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			fields = append(fields, fieldError{Field: "openAt", Code: "invalid"})
		} else {
			f.openAt = &instant
		}
	}

	return f, fields
}

// searchTerm validates the 2..80 character bound both text questions share
// (S21), counted in characters so accented names are not measured in bytes.
func searchTerm(raw, field string, fields *[]fieldError) string {
	term := strings.TrimSpace(raw)
	if term == "" {
		return ""
	}
	switch n := utf8.RuneCountInString(term); {
	case n < 2:
		*fields = append(*fields, fieldError{Field: field, Code: "too_short"})
	case n > 80:
		*fields = append(*fields, fieldError{Field: field, Code: "too_long"})
	}
	return term
}

// attributeFilter parses the comma-separated conjunctive filter (S18): empty
// entries are ignored, duplicates collapse (the SQL counts the intersection
// against the requested cardinality, so a repeated code must count once), and
// a code the governed vocabulary does not know is rejected as unknown.
func attributeFilter(raw string, fields *[]fieldError) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	seen := make(map[string]bool)
	var codes []string
	unknown := false
	for _, part := range strings.Split(raw, ",") {
		code := strings.TrimSpace(part)
		if code == "" {
			continue
		}
		if !places.KnownAttribute(code) {
			unknown = true
			continue
		}
		if !seen[code] {
			seen[code] = true
			codes = append(codes, code)
		}
	}
	if unknown {
		*fields = append(*fields, fieldError{Field: "attributes", Code: "unknown"})
	}
	return codes
}

// places serves GET /v1/places: one route, one locator, two response shapes.
func (h *placesHandlers) places(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	nearRaw := strings.TrimSpace(query.Get("near"))
	bboxRaw := strings.TrimSpace(query.Get("bbox"))

	switch {
	case nearRaw == "" && bboxRaw == "":
		// One locator is required: neither is a validated error, never a silent default.
		writeValidationError(w, []fieldError{{Field: "locator", Code: "required"}})
	case nearRaw != "" && bboxRaw != "":
		// Both locators: the list and the map are different questions, so the request
		// is ambiguous rather than "prefer one".
		writeValidationError(w, []fieldError{{Field: "bbox", Code: "invalid"}})
	case bboxRaw != "":
		h.mapMode(w, r)
	default:
		h.listMode(w, r, nearRaw)
	}
}

func (h *placesHandlers) listMode(w http.ResponseWriter, r *http.Request, nearRaw string) {
	var fields []fieldError

	near, issue := geo.ParseCoordinate("near", nearRaw)
	if issue != nil {
		fields = append(fields, fieldError{Field: issue.Field, Code: issue.Code})
	}
	radius, issue := geo.ParseRadius(r.URL.Query().Get("radius"))
	if issue != nil {
		fields = append(fields, fieldError{Field: issue.Field, Code: issue.Code})
	}
	f, filterFields := filters(r)
	fields = append(fields, filterFields...)

	sort := strings.TrimSpace(r.URL.Query().Get("sort"))
	if sort == "" {
		sort = places.SortDistance
	}
	if !sortValues[sort] {
		fields = append(fields, fieldError{Field: "sort", Code: "invalid"})
	}

	if len(fields) > 0 {
		writeValidationError(w, fields)
		return
	}

	page, limit := pageAndLimit(r)
	result, err := h.discovery.List(r.Context(), places.ListQuery{
		Near:       near,
		Radius:     radius,
		Country:    f.country,
		Region:     f.region,
		Category:   f.category,
		Q:          f.q,
		Dish:       f.dish,
		Attributes: f.attributes,
		MaxPrice:   f.maxPrice,
		OpenAt:     f.openAt,
		Sort:       sort,
		Page:       page,
		Limit:      limit,
	})
	if err != nil {
		h.logger.Error("place discovery failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}

	writeJSON(w, http.StatusOK, listResponse(result))
}

func (h *placesHandlers) mapMode(w http.ResponseWriter, r *http.Request) {
	var fields []fieldError

	box, issue := geo.ParseBBox(strings.TrimSpace(r.URL.Query().Get("bbox")))
	if issue != nil {
		fields = append(fields, fieldError{Field: issue.Field, Code: issue.Code})
	}

	// The zoom is what selects the cluster resolution, so it is required in map mode
	// rather than defaulted: a default would silently group at a resolution the client
	// did not ask for.
	zoomRaw := strings.TrimSpace(r.URL.Query().Get("zoom"))
	var zoom int
	if zoomRaw == "" {
		fields = append(fields, fieldError{Field: "zoom", Code: "required"})
	} else if parsed, issue := geo.ParseZoom(zoomRaw); issue != nil {
		fields = append(fields, fieldError{Field: issue.Field, Code: issue.Code})
	} else {
		zoom = parsed
	}

	f, filterFields := filters(r)
	fields = append(fields, filterFields...)

	if len(fields) > 0 {
		writeValidationError(w, fields)
		return
	}

	result, err := h.discovery.Map(r.Context(), places.MapQuery{
		Box:        box,
		Zoom:       zoom,
		Resolution: geo.ResolutionForZoom(zoom),
		Country:    f.country,
		Region:     f.region,
		Category:   f.category,
		Q:          f.q,
		Dish:       f.dish,
		Attributes: f.attributes,
		MaxPrice:   f.maxPrice,
		OpenAt:     f.openAt,
	})
	if err != nil {
		h.logger.Error("place map failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}

	writeJSON(w, http.StatusOK, mapResponse(zoom, result))
}

// listPlace is one row of list mode as the contract documents it (openapi.yaml
// PlaceSummary). The legal name and the owner are absent by construction (S24 spirit).
type listPlace struct {
	CoPlace   string       `json:"coPlace"`
	Name      string       `json:"name"`
	Category  categoryRef  `json:"category"`
	Country   string       `json:"country"`
	Region    *string      `json:"region"`
	Latitude  float64      `json:"latitude"`
	Longitude float64      `json:"longitude"`
	Address   *string      `json:"address"`
	DistanceM *int         `json:"distanceMeters"`
	Rating    *placeRating `json:"rating"`
	TrendRank *int         `json:"trendingRank"`
}

type categoryRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type placeRating struct {
	Average float64 `json:"average"`
	Count   int     `json:"count"`
}

type listResponseBody struct {
	Mode        string      `json:"mode"`
	Page        int         `json:"page"`
	Limit       int         `json:"limit"`
	Total       int         `json:"total"`
	SortApplied string      `json:"sortApplied"`
	Degraded    bool        `json:"degraded"`
	Data        []listPlace `json:"data"`
}

// listResponse maps the service result onto the documented list body. Distances are
// reported in whole metres and ratings stay null when nothing was published: an
// unknown aggregate is never rendered as 0.
func listResponse(result places.ListResult) listResponseBody {
	data := make([]listPlace, 0, len(result.Rows))
	for _, row := range result.Rows {
		place := listPlace{
			CoPlace:   row.ID,
			Name:      row.Title,
			Category:  categoryRef{ID: row.CategoryID, Name: row.CategoryName},
			Country:   row.Country,
			Region:    row.Region,
			Latitude:  row.Latitude,
			Longitude: row.Longitude,
			Address:   row.Address,
			TrendRank: row.TrendingRank,
		}
		distance := int(math.Round(row.DistanceM))
		place.DistanceM = &distance
		if row.Rating != nil {
			place.Rating = &placeRating{Average: row.Rating.Average, Count: row.Rating.Count}
		}
		data = append(data, place)
	}
	return listResponseBody{
		Mode:        "list",
		Page:        result.Page,
		Limit:       result.Limit,
		Total:       result.Total,
		SortApplied: result.SortApplied,
		Degraded:    result.Degraded,
		Data:        data,
	}
}

type clusterCenter struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type mapCluster struct {
	Cell          string        `json:"cell"`
	Resolution    int           `json:"resolution"`
	Count         int           `json:"count"`
	Center        clusterCenter `json:"center"`
	AverageRating *float64      `json:"averageRating"`
}

type mapResponseBody struct {
	Mode       string       `json:"mode"`
	Zoom       int          `json:"zoom"`
	Resolution int          `json:"resolution"`
	Total      int          `json:"total"`
	Truncated  bool         `json:"truncated"`
	Clusters   []mapCluster `json:"clusters"`
}

// mapResponse maps the clustered viewport. A cluster carries counts and neutral
// aggregates only: no name, no district, no administrative claim (S20/D14).
func mapResponse(zoom int, result places.MapResult) mapResponseBody {
	clusters := make([]mapCluster, 0, len(result.Clusters))
	resolution := 0
	for _, cluster := range result.Clusters {
		if resolution == 0 {
			resolution = cluster.Resolution
		}
		clusters = append(clusters, mapCluster{
			Cell:          cluster.Cell,
			Resolution:    cluster.Resolution,
			Count:         cluster.Count,
			Center:        clusterCenter{Latitude: cluster.CenterLat, Longitude: cluster.CenterLng},
			AverageRating: cluster.AverageRating,
		})
	}
	if resolution == 0 {
		resolution = geo.ResolutionForZoom(zoom)
	}
	return mapResponseBody{
		Mode:       "map",
		Zoom:       zoom,
		Resolution: resolution,
		Total:      result.Total,
		Truncated:  result.Truncated,
		Clusters:   clusters,
	}
}
