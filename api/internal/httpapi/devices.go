package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/device"
)

// DeviceRegistry is where a push can be delivered.
type DeviceRegistry interface {
	Register(ctx context.Context, userID, token, platform string) (device.Device, error)
	Unregister(ctx context.Context, userID, deviceID string) error
}

type deviceHandlers struct {
	registry DeviceRegistry
	logger   *slog.Logger
}

type deviceRequest struct {
	Token    string `json:"token"`
	Platform string `json:"platform"`
}

type deviceResponse struct {
	CoDevice   string    `json:"coDevice"`
	Platform   string    `json:"platform"`
	LastSeenAt time.Time `json:"lastSeenAt"`
}

// register serves POST /v1/devices: the client tells the API where it can be reached.
//
// It is idempotent by token. Re-registering the same device — every app start, and whenever the
// provider rotates the token — updates the row instead of accumulating one per launch, and a
// phone that changes hands moves to the account holding it now.
func (h *deviceHandlers) register(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}

	var body deviceRequest
	if !decodeJSON(w, r, 8<<10, &body) {
		return
	}

	registered, err := h.registry.Register(r.Context(), userID, body.Token, body.Platform)
	if err != nil {
		var invalid device.ErrInvalid
		if errors.As(err, &invalid) {
			writeValidationError(w, []fieldError{{Field: invalid.Field, Code: invalid.Code}})
			return
		}
		h.logger.Error("registering a device failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}

	writeJSON(w, http.StatusCreated, deviceResponse{
		CoDevice:   registered.ID,
		Platform:   registered.Platform,
		LastSeenAt: registered.LastSeenAt,
	})
}

// unregister serves DELETE /v1/devices/{coDevice}. The answer is 404 for a device that does not
// exist, was already removed, or belongs to someone else — all three are the same fact to a
// client, and only the last one is a probe worth refusing.
func (h *deviceHandlers) unregister(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}

	deviceID := r.PathValue("coDevice")
	if !uuidPattern.MatchString(deviceID) {
		writeError(w, http.StatusNotFound, "not_found", "Device not found.")
		return
	}

	if err := h.registry.Unregister(r.Context(), userID, deviceID); err != nil {
		if errors.Is(err, device.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Device not found.")
			return
		}
		h.logger.Error("unregistering a device failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
