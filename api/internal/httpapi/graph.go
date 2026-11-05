package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/checkin"
	"github.com/ivvanare/ecosystem_complete/api/internal/follow"
	"github.com/ivvanare/ecosystem_complete/api/internal/geo"
)

// SocialGraph is the follow surface.
type SocialGraph interface {
	Follow(ctx context.Context, follower, followed string) error
	Unfollow(ctx context.Context, follower, followed string) error
	List(ctx context.Context, viewerID, direction string, page, limit int) (follow.Page, error)
}

// Checkins is the check-in surface.
type Checkins interface {
	Create(ctx context.Context, userID, placeID string, where geo.Coordinate, now time.Time) (checkin.Checkin, error)
}

type graphHandlers struct {
	follows  SocialGraph
	checkins Checkins
	parser   TokenParser
	logger   *slog.Logger
}

// follower reads the authenticated caller of a follow route.
func (h *graphHandlers) follower(w http.ResponseWriter, r *http.Request) (string, bool) {
	if h.parser == nil {
		writeError(w, http.StatusNotFound, "not_found", "Not found.")
		return "", false
	}
	token, ok := bearerToken(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return "", false
	}
	claims, err := h.parser.Parse(token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return "", false
	}
	return claims.Subject, true
}

// followRequest is the body of POST /v1/follows.
type followRequest struct {
	CoUser string `json:"coUser"`
}

// createFollow serves POST /v1/follows.
func (h *graphHandlers) createFollow(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.follower(w, r)
	if !ok {
		return
	}
	var body followRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "The body is not valid JSON.")
		return
	}
	if !uuidPattern.MatchString(strings.TrimSpace(body.CoUser)) {
		writeValidationError(w, []fieldError{{Field: "coUser", Code: "invalid"}})
		return
	}

	switch err := h.follows.Follow(r.Context(), userID, body.CoUser); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, follow.ErrSelfFollow):
		writeValidationError(w, []fieldError{{Field: "coUser", Code: "invalid"}})
	case errors.Is(err, follow.ErrUnknownUser):
		writeError(w, http.StatusNotFound, "not_found", "User not found.")
	default:
		h.logger.Error("following user failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}

// deleteFollow serves DELETE /v1/follows/{coUser}. Idempotent: removing an edge that is not
// there is a success, because the graph is a set.
func (h *graphHandlers) deleteFollow(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.follower(w, r)
	if !ok {
		return
	}
	target := r.PathValue("coUser")
	if !uuidPattern.MatchString(target) {
		writeValidationError(w, []fieldError{{Field: "coUser", Code: "invalid"}})
		return
	}
	switch err := h.follows.Unfollow(r.Context(), userID, target); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, follow.ErrSelfFollow):
		writeValidationError(w, []fieldError{{Field: "coUser", Code: "invalid"}})
	default:
		h.logger.Error("unfollowing user failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}

// listFollows serves GET /v1/follows?direction=following|followers.
func (h *graphHandlers) listFollows(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.follower(w, r)
	if !ok {
		return
	}
	direction := strings.TrimSpace(r.URL.Query().Get("direction"))
	if direction == "" {
		direction = follow.DirectionFollowing
	}
	if direction != follow.DirectionFollowing && direction != follow.DirectionFollowers {
		writeValidationError(w, []fieldError{{Field: "direction", Code: "invalid"}})
		return
	}
	page, limit := pageAndLimit(r)

	result, err := h.follows.List(r.Context(), userID, direction, page, limit)
	if err != nil {
		h.logger.Error("listing graph failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}

	data := make([]map[string]any, 0, len(result.Members))
	for _, member := range result.Members {
		// Mutual is the computed "friend" (S2): the same row is a friend for one viewer and
		// a stranger for another, so it can never be a stored column.
		data = append(data, map[string]any{
			"coUser": member.UserID, "name": member.Name, "mutual": member.Mutual,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"direction": direction, "page": result.Page, "limit": result.Limit,
		"total": result.Total, "data": data,
	})
}

// checkinRequest is the body of POST /v1/places/{coPlace}/checkins.
type checkinRequest struct {
	Near string `json:"near"`
}

// createCheckin serves POST /v1/places/{coPlace}/checkins.
//
// The coordinate proves presence and is then discarded: only the verification verdict is
// stored (resolved Q1, D11). A claim outside the radius is refused rather than downgraded.
func (h *graphHandlers) createCheckin(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.follower(w, r)
	if !ok {
		return
	}
	placeID, ok := placeParam(w, r)
	if !ok {
		return
	}
	var body checkinRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "The body is not valid JSON.")
		return
	}
	where, issue := geo.ParseCoordinate("near", body.Near)
	if issue != nil {
		writeValidationError(w, []fieldError{{Field: issue.Field, Code: issue.Code}})
		return
	}

	stored, err := h.checkins.Create(r.Context(), userID, placeID, where, time.Now().UTC())
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, map[string]any{
			"coCheckin": stored.ID, "coPlace": stored.PlaceID,
			"verification": stored.Verification, "createdAt": stored.CreatedAt,
		})
	case errors.Is(err, checkin.ErrNotNearPlace):
		// Not a validation error about the coordinate: the input was fine, the claim was
		// false. The code says so, so a client can explain it without parsing a sentence.
		writeJSON(w, http.StatusUnprocessableEntity, errorEnvelope{Error: errorBody{
			Code:    "not_near_place",
			Message: "You are not within the radius of this place.",
			Details: map[string]any{"fields": []fieldError{{Field: "near", Code: "invalid"}}},
		}})
	case errors.Is(err, checkin.ErrPlaceNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Place not found.")
	case errors.Is(err, checkin.ErrAlreadyCheckedIn):
		// A second check-in on the same day is a distinct condition, so it gets its own
		// code: reusing another domain's code would make a client branch on a lie.
		writeError(w, http.StatusConflict, "already_checked_in", "You already checked in to this place today.")
	default:
		h.logger.Error("checking in failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}
