package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/avatar"
)

// AvatarWriter is the one picture an account publishes about itself.
type AvatarWriter interface {
	Set(ctx context.Context, userID, url string) (*string, error)
	Clear(ctx context.Context, userID string) (*string, error)
}

type avatarHandlers struct {
	avatars AvatarWriter
	storage Storage
	logger  *slog.Logger
}

// upload serves PUT /v1/me/avatar as multipart/form-data: `file`, an image.
//
// It is a PUT, not a POST, because there is exactly one avatar per account: the same request twice
// leaves the same state, and there is no identifier to collect on the client.
func (h *avatarHandlers) upload(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, MaxPhotoBytes)
	if err := r.ParseMultipartForm(MaxPhotoBytes); err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			writeValidationError(w, []fieldError{{Field: "file", Code: "too_long"}})
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body is not valid form data.")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeValidationError(w, []fieldError{{Field: "file", Code: "required"}})
		return
	}
	defer func() { _ = file.Close() }()

	// The declared type is not trusted on its own: sniff the bytes and require both to agree, so
	// a renamed executable is not stored as a picture.
	extension, allowed := allowedPhotoTypes[header.Header.Get("Content-Type")]
	if !allowed {
		head := make([]byte, 512)
		read, _ := io.ReadFull(file, head)
		detected := http.DetectContentType(head[:read])
		extension, allowed = allowedPhotoTypes[detected]
		if !allowed {
			writeValidationError(w, []fieldError{{Field: "file", Code: "invalid"}})
			return
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
	}

	key := path.Join("avatars", userID, fmt.Sprintf("%d%s", time.Now().UnixNano(), extension))
	url, err := h.storage.Save(r.Context(), key, file)
	if err != nil {
		h.logger.Error("storing an avatar failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}

	previous, err := h.avatars.Set(r.Context(), userID, url)
	if err != nil {
		// The bytes are already written; removing them keeps storage from filling with orphans.
		_ = h.storage.Delete(r.Context(), key)
		h.writeAvatarError(w, err, "recording an avatar failed")
		return
	}
	// The replaced file is removed after the write succeeds: an avatar is replaced often, and
	// nothing references the old object anymore.
	h.deleteObject(r.Context(), previous)

	writeJSON(w, http.StatusOK, map[string]string{"avatarUrl": url})
}

// remove serves DELETE /v1/me/avatar. It is idempotent: clearing an avatar that was never set is
// a success, because the state the caller asked for is the state that now holds.
func (h *avatarHandlers) remove(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}

	previous, err := h.avatars.Clear(r.Context(), userID)
	if err != nil {
		h.writeAvatarError(w, err, "clearing an avatar failed")
		return
	}
	h.deleteObject(r.Context(), previous)
	w.WriteHeader(http.StatusNoContent)
}

func (h *avatarHandlers) writeAvatarError(w http.ResponseWriter, err error, message string) {
	if errors.Is(err, avatar.ErrNotFound) {
		// A valid token for an account that is gone is an authentication problem, not a 404.
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	h.logger.Error(message, slog.String("error", err.Error()))
	writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
}

// deleteObject removes the file a stored URL points at, best effort. A URL that does not look like
// one of ours is left alone: guessing at keys is how a delete endpoint becomes a liability.
func (h *avatarHandlers) deleteObject(ctx context.Context, url *string) {
	if url == nil || *url == "" {
		return
	}
	if err := h.storage.Delete(ctx, mediaKey(*url)); err != nil {
		h.logger.Warn("removing a replaced avatar failed", slog.String("error", err.Error()))
	}
}

// mediaKey turns a public URL back into the storage key it came from.
func mediaKey(url string) string {
	return strings.TrimPrefix(strings.TrimPrefix(url, "/"), "media/")
}
