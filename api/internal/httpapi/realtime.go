package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/ivvanare/ecosystem_complete/api/internal/plans"
	"github.com/ivvanare/ecosystem_complete/api/internal/realtime"
)

// socketIDPattern is the shape Soketi hands the client on connect ("123.456"). It is validated
// because it is signed verbatim: anything that is not a socket id is a request to sign a string
// of the caller's choosing.
var socketIDPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)

// realtimeHandlers serves the subscription authorization. The channel is private, so a client
// cannot subscribe until this API says it may: that is the whole reason the route exists.
type realtimeHandlers struct {
	publisher realtime.Publisher
	plans     PlanCollaboration
	logger    *slog.Logger
}

// auth serves POST /v1/realtime/auth: `{socketId, channel}` in, `{auth}` out.
//
// The decision is the plan's own visibility door, not a second copy of it: a member may watch, a
// public plan may be watched by any authenticated account, and a private plan answers 404 to
// everyone else so it never confirms that it exists (S3).
func (h *realtimeHandlers) auth(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}

	var body struct {
		SocketID string `json:"socketId"`
		Channel  string `json:"channel"`
	}
	if !decodeJSON(w, r, 4<<10, &body) {
		return
	}
	socketID := strings.TrimSpace(body.SocketID)
	if socketID == "" {
		writeValidationError(w, []fieldError{{Field: "socketId", Code: "required"}})
		return
	}
	if !socketIDPattern.MatchString(socketID) {
		writeValidationError(w, []fieldError{{Field: "socketId", Code: "invalid"}})
		return
	}
	channel := strings.TrimSpace(body.Channel)
	planID, ours := realtime.ParseChannel(channel)
	if !ours || !uuidPattern.MatchString(planID) {
		writeValidationError(w, []fieldError{{Field: "channel", Code: "invalid"}})
		return
	}

	if err := h.plans.CanWatch(r.Context(), userID, planID); err != nil {
		if errors.Is(err, plans.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Plan not found.")
			return
		}
		h.logger.Error("authorizing a subscription failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}

	auth, err := h.publisher.SubscriptionAuth(socketID, channel)
	if err != nil {
		// A channel that reached here has already been proven to be ours, so this is a wiring
		// fault: signing must not fail for a valid request.
		h.logger.Error("signing a subscription failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"auth": auth})
}
