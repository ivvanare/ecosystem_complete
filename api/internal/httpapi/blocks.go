package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/blocks"
)

// Blocking is the block surface (decision Q2).
type Blocking interface {
	Block(ctx context.Context, blocker, blocked string) error
	Unblock(ctx context.Context, blocker, blocked string) error
	List(ctx context.Context, blocker string) ([]blocks.Block, error)
}

type blockHandlers struct {
	service Blocking
	logger  *slog.Logger
}

func (h *blockHandlers) caller(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID, ok := userIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return "", false
	}
	return userID, true
}

type blockRequest struct {
	CoUser string `json:"coUser"`
}

// create serves POST /v1/blocks.
func (h *blockHandlers) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.caller(w, r)
	if !ok {
		return
	}
	var body blockRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body is not valid JSON.")
		return
	}
	if !uuidPattern.MatchString(strings.TrimSpace(body.CoUser)) {
		writeValidationError(w, []fieldError{{Field: "coUser", Code: "invalid"}})
		return
	}
	switch err := h.service.Block(r.Context(), userID, body.CoUser); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, blocks.ErrSelfBlock):
		writeValidationError(w, []fieldError{{Field: "coUser", Code: "invalid"}})
	default:
		h.logger.Error("blocking user failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}

// remove serves DELETE /v1/blocks/{coUser}: idempotent, like unfollowing.
func (h *blockHandlers) remove(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.caller(w, r)
	if !ok {
		return
	}
	target := r.PathValue("coUser")
	if !uuidPattern.MatchString(target) {
		writeValidationError(w, []fieldError{{Field: "coUser", Code: "invalid"}})
		return
	}
	switch err := h.service.Unblock(r.Context(), userID, target); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, blocks.ErrSelfBlock):
		writeValidationError(w, []fieldError{{Field: "coUser", Code: "invalid"}})
	default:
		h.logger.Error("unblocking user failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}

// listed serves GET /v1/blocks: whom the caller blocks.
func (h *blockHandlers) listed(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.caller(w, r)
	if !ok {
		return
	}
	rows, err := h.service.List(r.Context(), userID)
	if err != nil {
		h.logger.Error("listing blocks failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}
	data := make([]map[string]any, 0, len(rows))
	for _, block := range rows {
		data = append(data, map[string]any{
			"coUser": block.UserID, "name": block.Name,
			"createdAt": block.CreatedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "total": len(data)})
}
