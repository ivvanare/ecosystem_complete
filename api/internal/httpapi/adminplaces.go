package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/placeadmin"
)

// PlaceBackoffice is the curated catalogue surface.
type PlaceBackoffice interface {
	List(ctx context.Context, viewer placeadmin.Viewer, filter placeadmin.Filter, page, limit int) (placeadmin.Page, error)
	Get(ctx context.Context, viewer placeadmin.Viewer, placeID string) (placeadmin.Place, error)
	Create(ctx context.Context, actorID string, in placeadmin.CreateInput) (placeadmin.Place, error)
	Update(ctx context.Context, viewer placeadmin.Viewer, actorID, placeID string, in placeadmin.UpdateInput) (placeadmin.Place, error)
	Retire(ctx context.Context, viewer placeadmin.Viewer, actorID, placeID string) error
	Categories(ctx context.Context, status string, page, limit int) ([]placeadmin.Category, int, error)
	Schedules(ctx context.Context, viewer placeadmin.Viewer, placeID string) ([]placeadmin.Schedule, error)
	ReplaceSchedules(ctx context.Context, viewer placeadmin.Viewer, actorID, placeID string, schedules []placeadmin.Schedule) error
	Attributes(ctx context.Context, viewer placeadmin.Viewer, placeID string) ([]string, error)
	ReplaceAttributes(ctx context.Context, viewer placeadmin.Viewer, actorID, placeID string, codes []string) error
	Menus(ctx context.Context, viewer placeadmin.Viewer, placeID string) ([]placeadmin.Menu, error)
	ReplaceMenus(ctx context.Context, viewer placeadmin.Viewer, actorID, placeID string, menus []placeadmin.Menu) error
	AttributeCatalogue(ctx context.Context) ([]placeadmin.Category, error)
	AddPhoto(ctx context.Context, viewer placeadmin.Viewer, actorID, placeID, url, photoType string, order int) (placeadmin.Photo, error)
	RemovePhoto(ctx context.Context, viewer placeadmin.Viewer, actorID, photoID string) error
}

// Permissions the places backoffice is built from. place.create/update/delete are seeded in
// 06-seeds.sql; admin.access is what separates the backoffice from an owner-editor.
const (
	PlaceCreatePermission = "place.create"
	PlaceUpdatePermission = "place.update"
	PlaceDeletePermission = "place.delete"
	// PlaceManageAllPermission is what separates an operator of the whole catalogue from an
	// owner who edits their own row. It is deliberately NOT admin.access: the seeded EDITOR
	// role already holds admin.access (it means "may enter the backoffice"), so scoping with
	// it would hand every editor the entire catalogue.
	PlaceManageAllPermission = "place.manage_all"
)

type placesAdminHandlers struct {
	service    PlaceBackoffice
	storage    Storage
	parser     TokenParser
	authorizer Authorizer
	logger     *slog.Logger
}

// viewer resolves who asks and whether they manage the whole catalogue.
//
// ManageAll comes from a live permission check, and a failing check degrades closed: the
// caller is treated as an owner, never as an admin (S1).
func (h *placesAdminHandlers) viewer(r *http.Request) (placeadmin.Viewer, bool) {
	token, ok := bearerToken(r)
	if !ok {
		return placeadmin.Viewer{}, false
	}
	claims, err := h.parser.Parse(token)
	if err != nil {
		return placeadmin.Viewer{}, false
	}
	viewer := placeadmin.Viewer{UserID: claims.Subject}
	if h.authorizer != nil {
		allowed, err := h.authorizer.Can(r.Context(), claims.Subject, PlaceManageAllPermission)
		if err != nil {
			h.logger.Warn("place.manage_all check failed, scope narrows to owner",
				slog.String("error", err.Error()))
		} else {
			viewer.ManageAll = allowed
		}
	}
	return viewer, true
}

// placeAdminParam validates the {coPlace} path value.
func placeAdminParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	placeID := r.PathValue("coPlace")
	if !uuidPattern.MatchString(placeID) {
		writeValidationError(w, []fieldError{{Field: "coPlace", Code: "invalid"}})
		return "", false
	}
	return placeID, true
}

// placeResponse is one place as the contract documents it.
type placeResponse struct {
	CoPlace     string  `json:"coPlace"`
	CoOwnerUser *string `json:"coOwnerUser"`
	Name        string  `json:"name"`
	Title       string  `json:"title"`
	Description *string `json:"description"`
	Category    struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"category"`
	Country   string  `json:"country"`
	Region    *string `json:"region"`
	Timezone  string  `json:"timezone"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Address   *string `json:"address"`
	Status    string  `json:"status"`
	// Content, populated by the detail read so the edit form fills with one call.
	Schedules  []placeadmin.Schedule `json:"schedules"`
	Attributes []string              `json:"attributes"`
	Menus      []placeadmin.Menu     `json:"menus"`
	Photos     []placeadmin.Photo    `json:"photos"`
	Rating     *placeRating          `json:"rating"`
	CreatedAt  time.Time             `json:"createdAt"`
	UpdatedAt  time.Time             `json:"updatedAt"`
	DeletedAt  *time.Time            `json:"deletedAt"`
}

func toPlaceResponse(place placeadmin.Place) placeResponse {
	response := placeResponse{
		CoPlace: place.ID, CoOwnerUser: place.OwnerUserID,
		Name: place.Name, Title: place.Title, Description: place.Description,
		Country: place.Country, Region: place.Region, Timezone: place.Timezone,
		Latitude: place.Latitude, Longitude: place.Longitude, Address: place.Address,
		Status: place.Status, Schedules: place.Schedules, Attributes: place.Attributes,
		Menus: place.Menus, Photos: place.Photos,
		Rating:    toAdminRating(place.Rating),
		CreatedAt: place.CreatedAt, UpdatedAt: place.UpdatedAt,
		DeletedAt: place.DeletedAt,
	}
	response.Category.ID = place.CategoryID
	response.Category.Name = place.CategoryName
	return response
}

// writePlaceError maps the service's sentinels onto the documented responses.
func (h *placesAdminHandlers) writePlaceError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, placeadmin.ErrNotFound):
		// Invisible and missing answer the same way: a caller who may not see a row must
		// not learn that it exists (S4).
		writeError(w, http.StatusNotFound, "not_found", "Place not found.")
	case errors.Is(err, placeadmin.ErrUnknownCategory):
		writeValidationError(w, []fieldError{{Field: "coPlaceCategory", Code: "unknown"}})
	default:
		h.logger.Error(action, slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}

// listed serves GET /v1/admin/places, scoped by ownership (S2).
func (h *placesAdminHandlers) listed(w http.ResponseWriter, r *http.Request) {
	viewer, ok := h.viewer(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	query := r.URL.Query()
	filter := placeadmin.Filter{
		Query:      strings.TrimSpace(query.Get("q")),
		Status:     strings.TrimSpace(query.Get("status")),
		CategoryID: strings.TrimSpace(query.Get("categoryId")),
		Country:    strings.TrimSpace(query.Get("country")),
		Region:     strings.TrimSpace(query.Get("region")),
	}

	var fields []fieldError
	validStatuses := map[string]bool{"": true, "ACTIVE": true, "INACTIVE": true, "DELETED": true}
	if !validStatuses[filter.Status] {
		fields = append(fields, fieldError{Field: "status", Code: "invalid"})
	}
	if filter.CategoryID != "" && !uuidPattern.MatchString(filter.CategoryID) {
		fields = append(fields, fieldError{Field: "categoryId", Code: "invalid"})
	}
	if filter.Country != "" && !countryPattern.MatchString(filter.Country) {
		fields = append(fields, fieldError{Field: "country", Code: "invalid"})
	}
	if filter.Region != "" && !regionPattern.MatchString(filter.Region) {
		fields = append(fields, fieldError{Field: "region", Code: "invalid"})
	}
	if len(fields) > 0 {
		writeValidationError(w, fields)
		return
	}

	page, limit := pageAndLimit(r)
	result, err := h.service.List(r.Context(), viewer, filter, page, limit)
	if err != nil {
		h.writePlaceError(w, err, "listing places")
		return
	}

	data := make([]placeResponse, 0, len(result.Rows))
	for _, place := range result.Rows {
		data = append(data, toPlaceResponse(place))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": data, "page": result.Page, "limit": result.Limit, "total": result.Total,
	})
}

// categories serves GET /v1/place-categories: the taxonomy, public.
//
// It is deliberately not behind admin: a client needs the same list to render a discovery
// filter, and the catalogue is not customer data. An unknown status is rejected rather than
// becoming a SQL enum error.
func (h *placesAdminHandlers) categories(w http.ResponseWriter, r *http.Request) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status != "" && status != "ACTIVE" && status != "INACTIVE" {
		writeValidationError(w, []fieldError{{Field: "status", Code: "invalid"}})
		return
	}
	page, limit := pageAndLimit(r)
	rows, total, err := h.service.Categories(r.Context(), status, page, limit)
	if err != nil {
		h.logger.Error("listing categories", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}

	data := make([]map[string]any, 0, len(rows))
	for _, category := range rows {
		data = append(data, map[string]any{
			"coPlaceCategory": category.ID, "code": category.Code,
			"name": category.Name, "status": category.Status,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": data, "page": page, "limit": limit, "total": total,
	})
}

// read serves GET /v1/admin/places/{coPlace}, scoped by ownership: an invisible row is 404.
func (h *placesAdminHandlers) read(w http.ResponseWriter, r *http.Request) {
	viewer, ok := h.viewer(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	placeID, ok := placeAdminParam(w, r)
	if !ok {
		return
	}
	place, err := h.service.Get(r.Context(), viewer, placeID)
	if err != nil {
		h.writePlaceError(w, err, "reading place")
		return
	}
	writeJSON(w, http.StatusOK, toPlaceResponse(place))
}

// placeWriteRequest is the body of create and update. Pointers distinguish "absent" from
// "set to null", which is what makes a partial update honest.
type placeWriteRequest struct {
	CoOwnerUser     *string  `json:"coOwnerUser"`
	Name            *string  `json:"name"`
	Title           *string  `json:"title"`
	Description     *string  `json:"description"`
	CoPlaceCategory *string  `json:"coPlaceCategory"`
	Country         *string  `json:"country"`
	Region          *string  `json:"region"`
	Timezone        *string  `json:"timezone"`
	Latitude        *float64 `json:"latitude"`
	Longitude       *float64 `json:"longitude"`
	Address         *string  `json:"address"`
	Status          *string  `json:"status"`
}

// validatePlaceShapes checks the model's shapes at the edge (S7).
func validatePlaceShapes(body placeWriteRequest, creating bool, fields *[]fieldError) {
	add := func(field, code string) { *fields = append(*fields, fieldError{Field: field, Code: code}) }

	if creating {
		for name, value := range map[string]*string{
			"name": body.Name, "title": body.Title, "coPlaceCategory": body.CoPlaceCategory,
			"country": body.Country, "timezone": body.Timezone,
		} {
			if value == nil || strings.TrimSpace(*value) == "" {
				add(name, "required")
			}
		}
		if body.Latitude == nil {
			add("latitude", "required")
		}
		if body.Longitude == nil {
			add("longitude", "required")
		}
	}
	if body.Name != nil && strings.TrimSpace(*body.Name) == "" {
		add("name", "required")
	}
	if body.Title != nil && strings.TrimSpace(*body.Title) == "" {
		add("title", "required")
	}
	if body.Country != nil && !countryPattern.MatchString(*body.Country) {
		add("country", "invalid")
	}
	if body.Region != nil && *body.Region != "" && !regionPattern.MatchString(*body.Region) {
		add("region", "invalid")
	}
	if body.CoPlaceCategory != nil && !uuidPattern.MatchString(*body.CoPlaceCategory) {
		add("coPlaceCategory", "invalid")
	}
	if body.CoOwnerUser != nil && *body.CoOwnerUser != "" && !uuidPattern.MatchString(*body.CoOwnerUser) {
		add("coOwnerUser", "invalid")
	}
	if body.Latitude != nil && (*body.Latitude < -90 || *body.Latitude > 90) {
		add("latitude", "invalid")
	}
	if body.Longitude != nil && (*body.Longitude < -180 || *body.Longitude > 180) {
		add("longitude", "invalid")
	}
	if body.Status != nil {
		if *body.Status != "ACTIVE" && *body.Status != "INACTIVE" {
			add("status", "invalid")
		}
	}
}

// create serves POST /v1/admin/places. Backoffice only (S3): the route is gated by
// place.manage_all, because the catalogue is curated and a self-service claim is another
// decision. admin.access would be too wide — every editor holds it.
func (h *placesAdminHandlers) create(w http.ResponseWriter, r *http.Request) {
	viewer, ok := h.viewer(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	var body placeWriteRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body is not valid JSON.")
		return
	}
	var fields []fieldError
	validatePlaceShapes(body, true, &fields)
	if len(fields) > 0 {
		writeValidationError(w, fields)
		return
	}

	place, err := h.service.Create(r.Context(), viewer.UserID, placeadmin.CreateInput{
		OwnerUserID: strPtr(body.CoOwnerUser),
		Name:        *body.Name,
		Title:       *body.Title,
		Description: strPtr(body.Description),
		CategoryID:  *body.CoPlaceCategory,
		Country:     *body.Country,
		Region:      strPtr(body.Region),
		Timezone:    *body.Timezone,
		Latitude:    *body.Latitude,
		Longitude:   *body.Longitude,
		Address:     strPtr(body.Address),
	})
	if err != nil {
		h.writePlaceError(w, err, "creating place")
		return
	}
	writeJSON(w, http.StatusCreated, toPlaceResponse(place))
}

// update serves PATCH /v1/admin/places/{coPlace}, scoped by ownership (S4).
func (h *placesAdminHandlers) update(w http.ResponseWriter, r *http.Request) {
	viewer, ok := h.viewer(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	placeID, ok := placeAdminParam(w, r)
	if !ok {
		return
	}
	var body placeWriteRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body is not valid JSON.")
		return
	}
	var fields []fieldError
	validatePlaceShapes(body, false, &fields)
	if len(fields) > 0 {
		writeValidationError(w, fields)
		return
	}

	// Only a backoffice operator may reassign the owner: ownership is not something an
	// owner hands over.
	var owner **string
	if body.CoOwnerUser != nil && viewer.ManageAll {
		owner = emptyToNil(body.CoOwnerUser)
	}

	place, err := h.service.Update(r.Context(), viewer, viewer.UserID, placeID, placeadmin.UpdateInput{
		Name:        body.Name,
		Title:       body.Title,
		Description: clearable(body.Description),
		CategoryID:  body.CoPlaceCategory,
		Country:     body.Country,
		Region:      clearable(body.Region),
		Timezone:    body.Timezone,
		Latitude:    body.Latitude,
		Longitude:   body.Longitude,
		Address:     clearable(body.Address),
		OwnerUserID: owner,
		Status:      body.Status,
	})
	if err != nil {
		h.writePlaceError(w, err, "updating place")
		return
	}
	writeJSON(w, http.StatusOK, toPlaceResponse(place))
}

// retire serves DELETE /v1/admin/places/{coPlace}: logical, never physical (S5).
func (h *placesAdminHandlers) retire(w http.ResponseWriter, r *http.Request) {
	viewer, ok := h.viewer(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	placeID, ok := placeAdminParam(w, r)
	if !ok {
		return
	}
	if err := h.service.Retire(r.Context(), viewer, viewer.UserID, placeID); err != nil {
		h.writePlaceError(w, err, "retiring place")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// strPtr turns an empty string into an absent value: the model keeps NULL for "no data",
// never an empty string that looks like data.
func strPtr(value *string) *string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	return value
}

// emptyToNil turns an empty string into an absent value: the model keeps NULL for "no data",
// never an empty string that looks like data.
func emptyToNil(value *string) **string {
	if value == nil {
		return nil
	}
	out := new(*string)
	if strings.TrimSpace(*value) == "" {
		*out = nil
		return out
	}
	*out = value
	return out
}

// clearable keeps "absent" and "explicitly null" apart, which is what a partial update needs.
func clearable(value *string) **string { return emptyToNil(value) }

// schedules serves GET and PUT /v1/admin/places/{coPlace}/schedules.
//
// Replacement, not per-slot editing: an operator edits "the hours of this place", and a
// half-applied weekly set would answer "closed" for a day nobody touched.
func (h *placesAdminHandlers) schedules(w http.ResponseWriter, r *http.Request) {
	viewer, ok := h.viewer(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	placeID, ok := placeAdminParam(w, r)
	if !ok {
		return
	}

	if r.Method == http.MethodGet {
		schedules, err := h.service.Schedules(r.Context(), viewer, placeID)
		if err != nil {
			h.writePlaceError(w, err, "reading schedules")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": schedules, "total": len(schedules)})
		return
	}

	var body struct {
		Schedules []placeadmin.Schedule `json:"schedules"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body is not valid JSON.")
		return
	}
	if err := h.service.ReplaceSchedules(r.Context(), viewer, viewer.UserID, placeID, body.Schedules); err != nil {
		if isScheduleValidation(err) {
			writeValidationError(w, []fieldError{{Field: "schedules", Code: "invalid"}})
			return
		}
		h.writePlaceError(w, err, "replacing schedules")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// attributes serves GET and PUT /v1/admin/places/{coPlace}/attributes.
func (h *placesAdminHandlers) attributes(w http.ResponseWriter, r *http.Request) {
	viewer, ok := h.viewer(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	placeID, ok := placeAdminParam(w, r)
	if !ok {
		return
	}

	if r.Method == http.MethodGet {
		codes, err := h.service.Attributes(r.Context(), viewer, placeID)
		if err != nil {
			h.writePlaceError(w, err, "reading attributes")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": codes, "total": len(codes)})
		return
	}

	var body struct {
		Codes []string `json:"codes"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body is not valid JSON.")
		return
	}
	if err := h.service.ReplaceAttributes(r.Context(), viewer, viewer.UserID, placeID, body.Codes); err != nil {
		var unknown *placeadmin.UnknownAttributeError
		if errors.As(err, &unknown) {
			// A dropped attribute the operator believes is set is worse than a rejected request.
			writeValidationError(w, []fieldError{{Field: "codes", Code: "unknown"}})
			return
		}
		h.writePlaceError(w, err, "replacing attributes")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// isScheduleValidation reports whether the failure was the edge validation of the hours.
func isScheduleValidation(err error) bool {
	return strings.Contains(err.Error(), "placeadmin:")
}

// menu serves GET and PUT /v1/admin/places/{coPlace}/menu: the whole card.
//
// Replacement, like the weekly hours: a menu applied in halves shows a price that a filter reads
// and a customer never sees.
func (h *placesAdminHandlers) menu(w http.ResponseWriter, r *http.Request) {
	viewer, ok := h.viewer(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	placeID, ok := placeAdminParam(w, r)
	if !ok {
		return
	}

	if r.Method == http.MethodGet {
		menus, err := h.service.Menus(r.Context(), viewer, placeID)
		if err != nil {
			h.writePlaceError(w, err, "reading the menu")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": menus, "total": len(menus)})
		return
	}

	var body struct {
		Menus []placeadmin.Menu `json:"menus"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body is not valid JSON.")
		return
	}
	if err := h.service.ReplaceMenus(r.Context(), viewer, viewer.UserID, placeID, body.Menus); err != nil {
		var unknownCurrency *placeadmin.UnknownCurrencyError
		switch {
		case errors.As(err, &unknownCurrency):
			writeValidationError(w, []fieldError{{Field: "menus", Code: "unknown"}})
		case strings.Contains(err.Error(), "placeadmin:"):
			// The model's shapes, caught at the edge so a negative price or a nameless item is a
			// 422 instead of a constraint violation the client cannot read.
			writeValidationError(w, []fieldError{{Field: "menus", Code: "invalid"}})
		default:
			h.writePlaceError(w, err, "replacing the menu")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// attributeCatalogue serves GET /v1/place-attributes: the vocabulary, public.
//
// It exists for the same reason the category catalogue does: the panel renders a select and the
// mobile client renders the same list as discovery filters, and hiding taxonomy behind a
// backoffice permission would have forced two routes with identical content.
func (h *placesAdminHandlers) attributeCatalogue(w http.ResponseWriter, r *http.Request) {
	rows, err := h.service.AttributeCatalogue(r.Context())
	if err != nil {
		h.logger.Error("listing the attribute catalogue failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}
	data := make([]map[string]any, 0, len(rows))
	for _, attribute := range rows {
		data = append(data, map[string]any{
			"coPlaceAttribute": attribute.ID, "code": attribute.Code,
			"name": attribute.Name, "status": attribute.Status,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "total": len(data)})
}

// toAdminRating maps the domain aggregate onto the documented shape.
func toAdminRating(rating *placeadmin.Rating) *placeRating {
	if rating == nil {
		return nil
	}
	return &placeRating{Average: rating.Average, Count: rating.Count}
}
