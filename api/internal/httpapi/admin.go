package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/audit"
	"github.com/ivvanare/ecosystem_complete/api/internal/identity"
)

// AdminReader is the administration surface.
type AdminReader interface {
	Users(ctx context.Context, filter identity.UserFilter) (identity.Page[identity.UserSummary], error)
	User(ctx context.Context, userID string) (identity.UserSummary, error)
	UpdateUser(ctx context.Context, actorID, userID string, name, status *string, meta audit.Event) (identity.UserSummary, error)
	SetRoles(ctx context.Context, actorID, userID string, roles []string, meta audit.Event) ([]string, error)
	AuditEvents(ctx context.Context, filter identity.AuditFilter) (identity.Page[identity.AuditEvent], error)
}

type userSummaryResponse struct {
	ID              string     `json:"id"`
	Name            *string    `json:"name"`
	Email           *string    `json:"email"`
	Status          string     `json:"status"`
	EmailVerifiedAt *time.Time `json:"email_verified_at"`
	Roles           []string   `json:"roles"`
	CreatedAt       time.Time  `json:"created_at"`
}

type auditEventResponse struct {
	ID        string         `json:"id"`
	Event     string         `json:"event"`
	UserID    *string        `json:"user_id"`
	ActorID   *string        `json:"actor_user_id"`
	EmailHash *string        `json:"email_hash"`
	IP        *string        `json:"ip"`
	Country   *string        `json:"country"`
	City      *string        `json:"city"`
	UserAgent *string        `json:"user_agent"`
	Metadata  map[string]any `json:"metadata"`
	CreatedAt time.Time      `json:"created_at"`
}

// InvitationSender emails an invitation to a future panel user.
type InvitationSender interface {
	Invite(ctx context.Context, email, name string) error
}

type adminHandlers struct {
	reader      AdminReader
	password    PasswordService
	invitations InvitationSender
}

func validUserScope(value string) bool {
	for _, candidate := range identity.UserScopes {
		if candidate == value {
			return true
		}
	}
	return false
}

func (h *adminHandlers) users(w http.ResponseWriter, r *http.Request) {
	page, limit := paging(r)
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope != "" && !validUserScope(scope) {
		// An unknown scope would silently match nobody; say so instead.
		writeValidationError(w, []fieldError{{Field: "scope", Code: "invalid"}})
		return
	}

	filter := identity.UserFilter{
		Page:   page,
		Limit:  limit,
		Query:  strings.TrimSpace(r.URL.Query().Get("q")),
		Status: strings.TrimSpace(r.URL.Query().Get("status")),
		Scope:  scope,
	}

	result, err := h.reader.Users(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}

	data := make([]userSummaryResponse, 0, len(result.Data))
	for _, user := range result.Data {
		data = append(data, toUserSummary(user))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": data, "page": result.Page, "limit": result.Limit, "total": result.Total,
	})
}

type inviteRequest struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

// inviteUser emails an invitation. Nothing is created here: the person registers and
// proves the address themselves, and the role is granted afterwards.
func (h *adminHandlers) inviteUser(w http.ResponseWriter, r *http.Request) {
	var body inviteRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body is not valid JSON.")
		return
	}

	email := strings.ToLower(strings.TrimSpace(body.Email))
	if _, err := mail.ParseAddress(email); err != nil {
		writeValidationError(w, []fieldError{{Field: "email", Code: "invalid"}})
		return
	}

	if err := h.invitations.Invite(r.Context(), email, strings.TrimSpace(body.Name)); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// resetUserPassword sends the account's reset code to its owner. An administrator never
// sets another password: the code proves the address, which keeps ownership with the user.
func (h *adminHandlers) resetUserPassword(w http.ResponseWriter, r *http.Request) {
	targetID := r.PathValue("userId")
	if targetID == "" {
		writeError(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}

	user, err := h.reader.User(r.Context(), targetID)
	if err != nil || user.Email == nil {
		// A user without an address (federated sign-in) has nothing to send a code to.
		writeError(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}

	if err := h.password.RequestReset(r.Context(), *user.Email); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

type updateUserRequest struct {
	Name   *string `json:"name"`
	Status *string `json:"status"`
}

// Mirrors public.suspendable_status in the model.
var validUserStatuses = []string{"ACTIVE", "INACTIVE", "SUSPENDED"}

func validUserStatus(value string) bool {
	for _, candidate := range validUserStatuses {
		if candidate == value {
			return true
		}
	}
	return false
}

// updateUser edits the fields an administrator owns. The email is not among them: it
// is proven before the row exists (diagram_base.txt:74), so changing it here would
// skip verification.
func (h *adminHandlers) updateUser(w http.ResponseWriter, r *http.Request) {
	actorID, ok := userIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}

	targetID := r.PathValue("userId")
	if targetID == "" {
		writeError(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}

	var body updateUserRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body is not valid JSON.")
		return
	}
	if body.Name == nil && body.Status == nil {
		writeValidationError(w, []fieldError{{Field: "name", Code: "required"}})
		return
	}
	if body.Name != nil {
		trimmed := strings.TrimSpace(*body.Name)
		if len(trimmed) > 120 {
			writeValidationError(w, []fieldError{{Field: "name", Code: "too_long"}})
			return
		}
		body.Name = &trimmed
	}
	if body.Status != nil && !validUserStatus(*body.Status) {
		writeValidationError(w, []fieldError{{Field: "status", Code: "invalid"}})
		return
	}

	meta := audit.Event{IP: clientIP(r), UserAgent: r.UserAgent()}
	user, err := h.reader.UpdateUser(r.Context(), actorID, targetID, body.Name, body.Status, meta)
	if err != nil {
		if errors.Is(err, identity.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Not found.")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}

	writeJSON(w, http.StatusOK, toUserSummary(user))
}

type setRolesRequest struct {
	Roles []string `json:"roles"`
}

// setRoles replaces the role set. It is the only endpoint that grants capability, so every
// change lands in the audit log with both the actor and the subject.
func (h *adminHandlers) setRoles(w http.ResponseWriter, r *http.Request) {
	actorID, ok := userIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}

	targetID := r.PathValue("userId")
	if targetID == "" {
		writeError(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}

	var body setRolesRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body is not valid JSON.")
		return
	}
	if body.Roles == nil {
		writeValidationError(w, []fieldError{{Field: "roles", Code: "required"}})
		return
	}

	meta := audit.Event{IP: clientIP(r), UserAgent: r.UserAgent()}

	if _, err := h.reader.SetRoles(r.Context(), actorID, targetID, body.Roles, meta); err != nil {
		switch {
		case errors.Is(err, identity.ErrUnknownRole):
			writeValidationError(w, []fieldError{{Field: "roles", Code: "unknown"}})
			return
		case errors.Is(err, identity.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Not found.")
			return
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
	}

	user, err := h.reader.User(r.Context(), targetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}

	writeJSON(w, http.StatusOK, toUserSummary(user))
}

func (h *adminHandlers) auditEvents(w http.ResponseWriter, r *http.Request) {
	page, limit := paging(r)
	query := r.URL.Query()

	filter := identity.AuditFilter{
		Page:   page,
		Limit:  limit,
		Event:  strings.TrimSpace(query.Get("event")),
		UserID: strings.TrimSpace(query.Get("userId")),
	}
	if raw := strings.TrimSpace(query.Get("from")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeValidationError(w, []fieldError{{Field: "from", Code: "invalid"}})
			return
		}
		filter.From = &parsed
	}
	if raw := strings.TrimSpace(query.Get("to")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeValidationError(w, []fieldError{{Field: "to", Code: "invalid"}})
			return
		}
		filter.To = &parsed
	}

	result, err := h.reader.AuditEvents(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}

	data := make([]auditEventResponse, 0, len(result.Data))
	for _, event := range result.Data {
		data = append(data, auditEventResponse{
			ID: event.ID, Event: event.Event, UserID: event.UserID, ActorID: event.ActorID,
			EmailHash: event.EmailHash, IP: event.IP, Country: event.Country, City: event.City,
			UserAgent: event.UserAgent, Metadata: event.Metadata, CreatedAt: event.CreatedAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": data, "page": result.Page, "limit": result.Limit, "total": result.Total,
	})
}

func toUserSummary(user identity.UserSummary) userSummaryResponse {
	return userSummaryResponse{
		ID:              user.ID,
		Name:            user.Name,
		Email:           user.Email,
		Status:          user.Status,
		EmailVerifiedAt: user.EmailVerifiedAt,
		Roles:           user.Roles,
		CreatedAt:       user.CreatedAt,
	}
}

// paging reads the contract's pagination defaults: page 1, limit 20, capped at 100.
func paging(r *http.Request) (int, int) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return page, limit
}
