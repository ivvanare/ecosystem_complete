package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/profile"
)

// ProfileReader is the public read of another account.
type ProfileReader interface {
	Profile(ctx context.Context, userID string) (profile.Profile, error)
}

type userProfileHandlers struct {
	reader ProfileReader
	logger *slog.Logger
}

type profileCounts struct {
	Reviews   int `json:"reviews"`
	Checkins  int `json:"checkins"`
	Followers int `json:"followers"`
	Following int `json:"following"`
}

type userProfileResponse struct {
	CoUser      string        `json:"coUser"`
	Name        *string       `json:"name"`
	AvatarURL   *string       `json:"avatarUrl"`
	MemberSince time.Time     `json:"memberSince"`
	Counts      profileCounts `json:"counts"`
}

// user serves GET /v1/users/{coUser}: the public identity of another person.
//
// It is public because every surface that leads here is public already — a review publishes its
// author as `{id, name}` — and because a follow button that opens nothing is not a feature.
//
// The email never appears: it is a credential, not a profile field. A suspended or deleted
// account answers 404 like one that never existed, so a moderation decision stays private.
func (h *userProfileHandlers) user(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("coUser")
	if !uuidPattern.MatchString(userID) {
		writeError(w, http.StatusNotFound, "not_found", "User not found.")
		return
	}

	account, err := h.reader.Profile(r.Context(), userID)
	if err != nil {
		if errors.Is(err, profile.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "User not found.")
			return
		}
		h.logger.Error("reading a profile failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}

	writeJSON(w, http.StatusOK, userProfileResponse{
		CoUser:      account.ID,
		Name:        account.Name,
		AvatarURL:   account.AvatarURL,
		MemberSince: account.MemberSince,
		Counts: profileCounts{
			Reviews:   account.Counts.Reviews,
			Checkins:  account.Counts.Checkins,
			Followers: account.Counts.Followers,
			Following: account.Counts.Following,
		},
	})
}
