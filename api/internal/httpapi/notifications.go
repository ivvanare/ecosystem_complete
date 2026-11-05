package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/ivvanare/ecosystem_complete/api/internal/notifications"
)

// NotificationPreferences is the opt-out surface (S13).
type NotificationPreferences interface {
	Preferences(ctx context.Context, userID string) (notifications.Preferences, error)
	SetPreference(ctx context.Context, userID, kind string, disabled bool) error
}

type notificationHandlers struct {
	service NotificationPreferences
	logger  *slog.Logger
}

// read serves GET /v1/notifications/preferences: the catalogue with the caller's toggles.
func (h *notificationHandlers) read(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	prefs, err := h.service.Preferences(r.Context(), userID)
	if err != nil {
		h.logger.Error("reading preferences failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}
	disabled := map[string]bool{}
	for _, kind := range prefs.Disabled {
		disabled[kind] = true
	}
	// The whole catalogue is returned, not just the disabled ones: a client renders a switch
	// per kind, and "absence means enabled" is a server rule, not something to infer.
	data := make([]map[string]any, 0, len(notifications.Kinds))
	for _, kind := range notifications.Kinds {
		data = append(data, map[string]any{"kind": kind, "enabled": !disabled[kind]})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "total": len(data)})
}

type preferenceRequest struct {
	Kind    string `json:"kind"`
	Enabled *bool  `json:"enabled"`
}

// update serves PATCH /v1/notifications/preferences.
func (h *notificationHandlers) update(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	var body preferenceRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body is not valid JSON.")
		return
	}
	var fields []fieldError
	if strings.TrimSpace(body.Kind) == "" {
		fields = append(fields, fieldError{Field: "kind", Code: "required"})
	} else if !notifications.IsKnownKind(body.Kind) {
		fields = append(fields, fieldError{Field: "kind", Code: "unknown"})
	}
	if body.Enabled == nil {
		fields = append(fields, fieldError{Field: "enabled", Code: "required"})
	}
	if len(fields) > 0 {
		writeValidationError(w, fields)
		return
	}

	if err := h.service.SetPreference(r.Context(), userID, body.Kind, !*body.Enabled); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		h.logger.Error("updating a preference failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
