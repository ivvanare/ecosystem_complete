package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/invitations"
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
	Invite(ctx context.Context, actorID, planID string, in invitations.CreateInput) (invitations.Invitation, error)
	PlanInvitations(ctx context.Context, actorID, planID string) ([]invitations.Invitation, error)
	MyInvitations(ctx context.Context, userID string) ([]invitations.Invitation, error)
	AcceptInvitation(ctx context.Context, actorID, invitationID string) error
	DeclineInvitation(ctx context.Context, actorID, invitationID string) error
}

// PlanModeration is the backoffice surface over plans and invitations (D21): it moderates,
// it never participates.
type PlanModeration interface {
	AdminList(ctx context.Context, filter plans.AdminFilter, page, limit int) ([]plans.Plan, int, error)
	Moderate(ctx context.Context, actorID, planID, status string) error
	InvitationAdmins(ctx context.Context, filter invitations.AdminFilter, page, limit int) ([]invitations.Invitation, int, error)
	RevokeInvitation(ctx context.Context, actorID, invitationID string) error
}

type planModerationHandlers struct {
	service PlanModeration
	logger  *slog.Logger
}

// adminPlans serves GET /v1/admin/plans.
func (h *planModerationHandlers) adminPlans(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	filter := plans.AdminFilter{
		Status:     strings.TrimSpace(query.Get("status")),
		OwnerID:    strings.TrimSpace(query.Get("ownerId")),
		Visibility: strings.TrimSpace(query.Get("visibility")),
	}
	var fields []fieldError
	if filter.Status != "" && filter.Status != "ACTIVE" && filter.Status != "INACTIVE" {
		fields = append(fields, fieldError{Field: "status", Code: "invalid"})
	}
	if filter.OwnerID != "" && !uuidPattern.MatchString(filter.OwnerID) {
		fields = append(fields, fieldError{Field: "ownerId", Code: "invalid"})
	}
	if filter.Visibility != "" && filter.Visibility != plans.VisibilityPublic && filter.Visibility != plans.VisibilityPrivate {
		fields = append(fields, fieldError{Field: "visibility", Code: "invalid"})
	}
	if len(fields) > 0 {
		writeValidationError(w, fields)
		return
	}

	page, limit := pageAndLimit(r)
	rows, total, err := h.service.AdminList(r.Context(), filter, page, limit)
	if err != nil {
		h.logger.Error("listing plans for moderation failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}
	data := make([]planResponse, 0, len(rows))
	for _, plan := range rows {
		data = append(data, toPlanResponse(plan))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "page": page, "limit": limit, "total": total})
}

type planStatusRequest struct {
	Status *string `json:"status"`
}

// moderatePlan serves PATCH /v1/admin/plans/{coRoute}.
func (h *planModerationHandlers) moderatePlan(w http.ResponseWriter, r *http.Request) {
	actorID, ok := userIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	planID, ok := planParam(w, r)
	if !ok {
		return
	}
	var body planStatusRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body is not valid JSON.")
		return
	}
	if body.Status == nil {
		writeValidationError(w, []fieldError{{Field: "status", Code: "required"}})
		return
	}
	if *body.Status != "ACTIVE" && *body.Status != "INACTIVE" {
		writeValidationError(w, []fieldError{{Field: "status", Code: "invalid"}})
		return
	}
	if err := h.service.Moderate(r.Context(), actorID, planID, *body.Status); err != nil {
		h.logger.Error("moderating a plan failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// adminInvitations serves GET /v1/admin/invitations.
func (h *planModerationHandlers) adminInvitations(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	filter := invitations.AdminFilter{
		Status:    strings.TrimSpace(query.Get("status")),
		PlanID:    strings.TrimSpace(query.Get("planId")),
		InviteeID: strings.TrimSpace(query.Get("inviteeId")),
	}
	var fields []fieldError
	if filter.PlanID != "" && !uuidPattern.MatchString(filter.PlanID) {
		fields = append(fields, fieldError{Field: "planId", Code: "invalid"})
	}
	if filter.InviteeID != "" && !uuidPattern.MatchString(filter.InviteeID) {
		fields = append(fields, fieldError{Field: "inviteeId", Code: "invalid"})
	}
	if len(fields) > 0 {
		writeValidationError(w, fields)
		return
	}

	page, limit := pageAndLimit(r)
	rows, total, err := h.service.InvitationAdmins(r.Context(), filter, page, limit)
	if err != nil {
		h.logger.Error("listing invitations for moderation failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}
	data := make([]invitationResponse, 0, len(rows))
	for _, invitation := range rows {
		data = append(data, toInvitationResponse(invitation))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "page": page, "limit": limit, "total": total})
}

// revokeInvitation serves DELETE /v1/admin/invitations/{coInvitation}.
func (h *planModerationHandlers) revokeInvitation(w http.ResponseWriter, r *http.Request) {
	actorID, ok := userIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	invitationID := r.PathValue("coInvitation")
	if !uuidPattern.MatchString(invitationID) {
		writeValidationError(w, []fieldError{{Field: "coInvitation", Code: "invalid"}})
		return
	}
	if err := h.service.RevokeInvitation(r.Context(), actorID, invitationID); err != nil {
		h.logger.Error("revoking an invitation failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Capabilities of the plan backoffice. They are not admin.access: that one only means "may
// enter the panel", and the seeded EDITOR role already holds it.
const (
	PlanModeratePermission       = "plan.moderate"
	InvitationModeratePermission = "invitation.moderate"
)

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

type invitationResponse struct {
	CoInvitation string     `json:"coInvitation"`
	CoRoute      string     `json:"coRoute"`
	CoUser       string     `json:"coUser"`
	Status       string     `json:"status"`
	Mode         string     `json:"mode"`
	Message      *string    `json:"message"`
	Hint         *string    `json:"hint"`
	ExpiresAt    time.Time  `json:"expiresAt"`
	Expired      bool       `json:"expired"`
	Revealed     bool       `json:"revealed"`
	CreatedAt    time.Time  `json:"createdAt"`
	RespondedAt  *time.Time `json:"respondedAt"`
	InviteeName  *string    `json:"inviteeName"`
	PlanName     *string    `json:"planName"`
	PlanAt       *time.Time `json:"planScheduledAt"`
}

func toInvitationResponse(i invitations.Invitation) invitationResponse {
	return invitationResponse{
		CoInvitation: i.ID, CoRoute: i.PlanID, CoUser: i.UserID, Status: i.Status, Mode: i.Mode,
		Message: i.Message, Hint: i.Hint, ExpiresAt: i.ExpiresAt, Expired: i.Expired,
		Revealed:  i.Revealed,
		CreatedAt: i.CreatedAt, RespondedAt: i.RespondedAt,
		InviteeName: i.InviteeName, PlanName: i.PlanName, PlanAt: i.PlanAt,
	}
}

type invitationRequest struct {
	CoUser    string  `json:"coUser"`
	ExpiresAt string  `json:"expiresAt"`
	Mode      *string `json:"mode"`
	Message   *string `json:"message"`
	Hint      *string `json:"hint"`
}

// invite serves POST /v1/plans/{coRoute}/invitations: EDITOR or OWNER only.
func (h *planHandlers) invite(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.caller(w, r)
	if !ok {
		return
	}
	planID, ok := planParam(w, r)
	if !ok {
		return
	}
	var body invitationRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body is not valid JSON.")
		return
	}
	var fields []fieldError
	if !uuidPattern.MatchString(strings.TrimSpace(body.CoUser)) {
		fields = append(fields, fieldError{Field: "coUser", Code: "invalid"})
	}
	if strings.TrimSpace(body.ExpiresAt) == "" {
		fields = append(fields, fieldError{Field: "expiresAt", Code: "required"})
	}
	var expiresAt time.Time
	if strings.TrimSpace(body.ExpiresAt) != "" {
		parsed, err := time.Parse(time.RFC3339, body.ExpiresAt)
		if err != nil {
			fields = append(fields, fieldError{Field: "expiresAt", Code: "invalid"})
		} else {
			expiresAt = parsed
		}
	}
	if body.Message != nil && len(*body.Message) > 500 {
		fields = append(fields, fieldError{Field: "message", Code: "too_long"})
	}
	if body.Hint != nil && len(*body.Hint) > 200 {
		fields = append(fields, fieldError{Field: "hint", Code: "too_long"})
	}
	if len(fields) > 0 {
		writeValidationError(w, fields)
		return
	}

	in := invitations.CreateInput{UserID: body.CoUser, ExpiresAt: expiresAt,
		Message: body.Message, Hint: body.Hint}
	if body.Mode != nil {
		in.Mode = *body.Mode
	}
	created, err := h.service.Invite(r.Context(), userID, planID, in)
	if err != nil {
		h.writeInvitationError(w, err, "inviting")
		return
	}
	writeJSON(w, http.StatusCreated, toInvitationResponse(created))
}

// writeInvitationError keeps 404 and 403 apart for the invitation routes too.
func (h *planHandlers) writeInvitationError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, invitations.ErrNotFound), errors.Is(err, invitations.ErrNotInvitee):
		writeError(w, http.StatusNotFound, "not_found", "Invitation not found.")
	case errors.Is(err, invitations.ErrForbidden), errors.Is(err, invitations.ErrNotMember):
		writeError(w, http.StatusForbidden, "permission_denied", "You do not have permission to perform this action.")
	case errors.Is(err, invitations.ErrExpiryNotFuture):
		writeValidationError(w, []fieldError{{Field: "expiresAt", Code: "invalid"}})
	case errors.Is(err, invitations.ErrSurpriseNeedsContent):
		writeValidationError(w, []fieldError{{Field: "hint", Code: "required"}})
	case errors.Is(err, invitations.ErrUnknownMode):
		writeValidationError(w, []fieldError{{Field: "mode", Code: "invalid"}})
	case errors.Is(err, invitations.ErrAlreadyPending):
		writeError(w, http.StatusConflict, "invitation_already_pending",
			"This person already has a live invitation to this plan.")
	default:
		h.logger.Error(action, slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}

// planInvitations serves GET /v1/plans/{coRoute}/invitations: the host's RSVP view.
func (h *planHandlers) planInvitations(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.caller(w, r)
	if !ok {
		return
	}
	planID, ok := planParam(w, r)
	if !ok {
		return
	}
	rows, err := h.service.PlanInvitations(r.Context(), userID, planID)
	if err != nil {
		h.writeInvitationError(w, err, "listing plan invitations")
		return
	}
	data := make([]invitationResponse, 0, len(rows))
	for _, invitation := range rows {
		data = append(data, toInvitationResponse(invitation))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "total": len(data)})
}

// myInvitations serves GET /v1/invitations: only the caller's own.
func (h *planHandlers) myInvitations(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.caller(w, r)
	if !ok {
		return
	}
	rows, err := h.service.MyInvitations(r.Context(), userID)
	if err != nil {
		h.writeInvitationError(w, err, "listing my invitations")
		return
	}
	data := make([]invitationResponse, 0, len(rows))
	for _, invitation := range rows {
		data = append(data, toInvitationResponse(invitation))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "total": len(data)})
}

// answerInvitation serves accept and decline; the path says which.
func (h *planHandlers) answerInvitation(accept bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := h.caller(w, r)
		if !ok {
			return
		}
		invitationID := r.PathValue("coInvitation")
		if !uuidPattern.MatchString(invitationID) {
			writeValidationError(w, []fieldError{{Field: "coInvitation", Code: "invalid"}})
			return
		}
		var err error
		if accept {
			err = h.service.AcceptInvitation(r.Context(), userID, invitationID)
		} else {
			err = h.service.DeclineInvitation(r.Context(), userID, invitationID)
		}
		if err != nil {
			h.writeInvitationError(w, err, "answering invitation")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// accept serves POST /v1/invitations/{coInvitation}/accept.
func (h *planHandlers) accept(w http.ResponseWriter, r *http.Request) { h.answerInvitation(true)(w, r) }

// decline serves POST /v1/invitations/{coInvitation}/decline.
func (h *planHandlers) decline(w http.ResponseWriter, r *http.Request) {
	h.answerInvitation(false)(w, r)
}
