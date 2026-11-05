package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/plans"
)

// PlanCollaboration is the plans surface.
type PlanCollaboration interface {
	Create(ctx context.Context, actorID string, in plans.CreateInput) (plans.Plan, error)
	Get(ctx context.Context, actorID, planID string) (plans.Plan, error)
	Update(ctx context.Context, actorID, planID string, in plans.UpdateInput) (plans.Plan, error)
	Join(ctx context.Context, actorID, planID string) error
	Leave(ctx context.Context, actorID, planID, targetID string) error
	List(ctx context.Context, actorID string, page, limit int) ([]plans.Plan, int, error)
	Places(ctx context.Context, actorID, planID string) ([]plans.PlanPlace, error)
	AddPlace(ctx context.Context, actorID, planID, placeID string, order int) error
	RemovePlace(ctx context.Context, actorID, planID, placeID string) error
	Vote(ctx context.Context, actorID, planID, placeID string, value int) error
}

type planHandlers struct {
	service PlanCollaboration
	logger  *slog.Logger
}

// caller reads the authenticated user of a plan route.
func (h *planHandlers) caller(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID, ok := userIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return "", false
	}
	return userID, true
}

func planParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	planID := r.PathValue("coRoute")
	if !uuidPattern.MatchString(planID) {
		writeValidationError(w, []fieldError{{Field: "coRoute", Code: "invalid"}})
		return "", false
	}
	return planID, true
}

// writePlanError maps the domain sentinels onto the documented responses. The 404/403 split is
// the whole point: an invisible plan must not confirm it exists, and a visible one the caller
// may not change must say so.
func (h *planHandlers) writePlanError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, plans.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Plan not found.")
	case errors.Is(err, plans.ErrForbidden):
		writeError(w, http.StatusForbidden, "permission_denied", "You do not have permission to perform this action.")
	default:
		h.logger.Error(action, slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}

type planResponse struct {
	CoRoute     string     `json:"coRoute"`
	CoUser      string     `json:"coUser"`
	Name        string     `json:"name"`
	Description *string    `json:"description"`
	Visibility  string     `json:"visibility"`
	Status      string     `json:"status"`
	ScheduledAt *time.Time `json:"scheduledAt"`
	PartySize   *int       `json:"partySize"`
	ViewerRole  string     `json:"viewerRole"`
	MemberCount int        `json:"memberCount"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

func toPlanResponse(plan plans.Plan) planResponse {
	return planResponse{
		CoRoute: plan.ID, CoUser: plan.CreatorID, Name: plan.Name, Description: plan.Description,
		Visibility: plan.Visibility, Status: plan.Status, ScheduledAt: plan.ScheduledAt,
		PartySize: plan.PartySize, ViewerRole: plan.ViewerRole, MemberCount: plan.MemberCount,
		CreatedAt: plan.CreatedAt, UpdatedAt: plan.UpdatedAt,
	}
}

type planWriteRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Visibility  *string `json:"visibility"`
	ScheduledAt *string `json:"scheduledAt"`
	PartySize   *int    `json:"partySize"`
}

// validatePlan checks the shapes the model enforces (S2, S25).
func validatePlan(body planWriteRequest, creating bool) []fieldError {
	var fields []fieldError
	add := func(field, code string) { fields = append(fields, fieldError{Field: field, Code: code}) }

	if creating && (body.Name == nil || strings.TrimSpace(*body.Name) == "") {
		add("name", "required")
	}
	if body.Name != nil && strings.TrimSpace(*body.Name) == "" {
		add("name", "required")
	}
	if body.Visibility != nil && *body.Visibility != plans.VisibilityPrivate && *body.Visibility != plans.VisibilityPublic {
		add("visibility", "invalid")
	}
	if body.PartySize != nil && (*body.PartySize < 1 || *body.PartySize > 50) {
		add("partySize", "invalid")
	}
	if body.ScheduledAt != nil && strings.TrimSpace(*body.ScheduledAt) != "" {
		if _, err := time.Parse(time.RFC3339, *body.ScheduledAt); err != nil {
			add("scheduledAt", "invalid")
		}
	}
	return fields
}

// create serves POST /v1/plans.
func (h *planHandlers) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.caller(w, r)
	if !ok {
		return
	}
	var body planWriteRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body is not valid JSON.")
		return
	}
	if fields := validatePlan(body, true); len(fields) > 0 {
		writeValidationError(w, fields)
		return
	}

	in := plans.CreateInput{Name: *body.Name}
	if body.Description != nil && strings.TrimSpace(*body.Description) != "" {
		in.Description = body.Description
	}
	if body.Visibility != nil {
		in.Visibility = *body.Visibility
	}
	if body.PartySize != nil {
		in.PartySize = body.PartySize
	}
	if body.ScheduledAt != nil && strings.TrimSpace(*body.ScheduledAt) != "" {
		parsed, _ := time.Parse(time.RFC3339, *body.ScheduledAt)
		in.ScheduledAt = &parsed
	}

	plan, err := h.service.Create(r.Context(), userID, in)
	if err != nil {
		h.writePlanError(w, err, "creating plan")
		return
	}
	writeJSON(w, http.StatusCreated, toPlanResponse(plan))
}

// read serves GET /v1/plans/{coRoute}.
func (h *planHandlers) read(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.caller(w, r)
	if !ok {
		return
	}
	planID, ok := planParam(w, r)
	if !ok {
		return
	}
	plan, err := h.service.Get(r.Context(), userID, planID)
	if err != nil {
		h.writePlanError(w, err, "reading plan")
		return
	}
	writeJSON(w, http.StatusOK, toPlanResponse(plan))
}

// update serves PATCH /v1/plans/{coRoute}.
func (h *planHandlers) update(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.caller(w, r)
	if !ok {
		return
	}
	planID, ok := planParam(w, r)
	if !ok {
		return
	}
	var body planWriteRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body is not valid JSON.")
		return
	}
	if fields := validatePlan(body, false); len(fields) > 0 {
		writeValidationError(w, fields)
		return
	}

	in := plans.UpdateInput{Name: body.Name, Visibility: body.Visibility}
	if body.Description != nil {
		clear := new(*string)
		if strings.TrimSpace(*body.Description) == "" {
			*clear = nil
		} else {
			*clear = body.Description
		}
		in.Description = clear
	}
	if body.ScheduledAt != nil {
		clear := new(*time.Time)
		if strings.TrimSpace(*body.ScheduledAt) != "" {
			parsed, _ := time.Parse(time.RFC3339, *body.ScheduledAt)
			*clear = &parsed
		}
		in.ScheduledAt = clear
	}
	if body.PartySize != nil {
		target := new(*int)
		value := *body.PartySize
		*target = &value
		in.PartySize = target
	}

	plan, err := h.service.Update(r.Context(), userID, planID, in)
	if err != nil {
		h.writePlanError(w, err, "updating plan")
		return
	}
	writeJSON(w, http.StatusOK, toPlanResponse(plan))
}

// join serves POST /v1/plans/{coRoute}/members: the self-join, always as VIEWER.
func (h *planHandlers) join(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.caller(w, r)
	if !ok {
		return
	}
	planID, ok := planParam(w, r)
	if !ok {
		return
	}
	if err := h.service.Join(r.Context(), userID, planID); err != nil {
		h.writePlanError(w, err, "joining plan")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// leave serves DELETE /v1/plans/{coRoute}/members/{coUser}.
func (h *planHandlers) leave(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.caller(w, r)
	if !ok {
		return
	}
	planID, ok := planParam(w, r)
	if !ok {
		return
	}
	target := r.PathValue("coUser")
	if !uuidPattern.MatchString(target) {
		writeValidationError(w, []fieldError{{Field: "coUser", Code: "invalid"}})
		return
	}
	if err := h.service.Leave(r.Context(), userID, planID, target); err != nil {
		h.writePlanError(w, err, "leaving plan")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listed serves GET /v1/plans: the plans the caller belongs to.
func (h *planHandlers) listed(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.caller(w, r)
	if !ok {
		return
	}
	page, limit := pageAndLimit(r)
	rows, total, err := h.service.List(r.Context(), userID, page, limit)
	if err != nil {
		h.writePlanError(w, err, "listing plans")
		return
	}
	data := make([]planResponse, 0, len(rows))
	for _, plan := range rows {
		data = append(data, toPlanResponse(plan))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": data, "page": page, "limit": limit, "total": total,
	})
}

// planPlaceResponse is one row of the itinerary.
type planPlaceResponse struct {
	CoPlace   string `json:"coPlace"`
	Title     string `json:"title"`
	Order     int    `json:"order"`
	NetVotes  int    `json:"netVotes"`
	MyVote    int    `json:"myVote"`
	CreatedAt string `json:"createdAt"`
}

// places serves GET /v1/plans/{coRoute}/places: the ordered itinerary.
func (h *planHandlers) places(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.caller(w, r)
	if !ok {
		return
	}
	planID, ok := planParam(w, r)
	if !ok {
		return
	}
	rows, err := h.service.Places(r.Context(), userID, planID)
	if err != nil {
		h.writePlanError(w, err, "reading plan places")
		return
	}
	data := make([]planPlaceResponse, 0, len(rows))
	for _, place := range rows {
		data = append(data, planPlaceResponse{
			CoPlace: place.PlaceID, Title: place.Title, Order: place.Order,
			NetVotes: place.NetVotes, MyVote: place.MyVote,
			CreatedAt: place.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "total": len(data)})
}

type planPlaceRequest struct {
	CoPlace string `json:"coPlace"`
	Order   *int   `json:"order"`
}

// addPlace serves POST /v1/plans/{coRoute}/places.
func (h *planHandlers) addPlace(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.caller(w, r)
	if !ok {
		return
	}
	planID, ok := planParam(w, r)
	if !ok {
		return
	}
	var body planPlaceRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body is not valid JSON.")
		return
	}
	if !uuidPattern.MatchString(strings.TrimSpace(body.CoPlace)) {
		writeValidationError(w, []fieldError{{Field: "coPlace", Code: "invalid"}})
		return
	}
	order := 1
	if body.Order != nil {
		order = *body.Order
	}
	if order < 1 {
		writeValidationError(w, []fieldError{{Field: "order", Code: "invalid"}})
		return
	}
	if err := h.service.AddPlace(r.Context(), userID, planID, body.CoPlace, order); err != nil {
		h.writePlanError(w, err, "adding place to plan")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// removePlace serves DELETE /v1/plans/{coRoute}/places/{coPlace}.
func (h *planHandlers) removePlace(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.caller(w, r)
	if !ok {
		return
	}
	planID, ok := planParam(w, r)
	if !ok {
		return
	}
	placeID := r.PathValue("coPlace")
	if !uuidPattern.MatchString(placeID) {
		writeValidationError(w, []fieldError{{Field: "coPlace", Code: "invalid"}})
		return
	}
	if err := h.service.RemovePlace(r.Context(), userID, planID, placeID); err != nil {
		h.writePlanError(w, err, "removing place from plan")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type voteRequest struct {
	Vote *int `json:"vote"`
}

// vote serves POST /v1/plans/{coRoute}/places/{coPlace}/votes.
func (h *planHandlers) vote(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.caller(w, r)
	if !ok {
		return
	}
	planID, ok := planParam(w, r)
	if !ok {
		return
	}
	placeID := r.PathValue("coPlace")
	if !uuidPattern.MatchString(placeID) {
		writeValidationError(w, []fieldError{{Field: "coPlace", Code: "invalid"}})
		return
	}
	var body voteRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body is not valid JSON.")
		return
	}
	if body.Vote == nil {
		writeValidationError(w, []fieldError{{Field: "vote", Code: "required"}})
		return
	}
	if *body.Vote != 1 && *body.Vote != -1 {
		writeValidationError(w, []fieldError{{Field: "vote", Code: "invalid"}})
		return
	}
	if err := h.service.Vote(r.Context(), userID, planID, placeID, *body.Vote); err != nil {
		h.writePlanError(w, err, "voting")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
