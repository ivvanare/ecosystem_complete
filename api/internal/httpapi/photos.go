package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/placeadmin"
)

// Storage is the file store the photo upload writes to. The interface lives here so the handler
// depends on the capability, not on the local implementation — swapping in S3 is one more
// implementation of the same three methods.
type Storage interface {
	Save(ctx context.Context, key string, body io.Reader) (string, error)
	Delete(ctx context.Context, key string) error
}

// MaxPhotoBytes bounds one upload. A catalogue image is not a video, and an unbounded body is a
// denial of service with extra steps.
const MaxPhotoBytes = 5 << 20

// allowedPhotoTypes maps an accepted content type to the extension it is stored with. The name
// on disk is ours, never the client's filename: that is a path traversal waiting to happen.
var allowedPhotoTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

// upload serves POST /v1/admin/places/{coPlace}/photos as multipart/form-data: `file`, an
// optional `type` (COVER|GALLERY, default GALLERY).
func (h *placesAdminHandlers) uploadPhoto(w http.ResponseWriter, r *http.Request) {
	viewer, ok := h.viewer(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	placeID, ok := placeAdminParam(w, r)
	if !ok {
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

	// The declared content type is not trusted alone: sniff the bytes and require both to agree,
	// so a renamed executable is not stored as an image.
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

	photoType := strings.ToUpper(strings.TrimSpace(r.FormValue("type")))
	if photoType == "" {
		photoType = placeadmin.PhotoGallery
	}
	if photoType != placeadmin.PhotoCover && photoType != placeadmin.PhotoGallery {
		writeValidationError(w, []fieldError{{Field: "type", Code: "invalid"}})
		return
	}

	key := path.Join("places", placeID, fmt.Sprintf("%d%s", time.Now().UnixNano(), extension))
	url, err := h.storage.Save(r.Context(), key, file)
	if err != nil {
		h.logger.Error("storing a photo failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}

	// `order` is read from the form, not hardcoded: the contract documents it, and a parameter
	// that silently does nothing is worse than one that does not exist.
	order := 0
	if raw := strings.TrimSpace(r.FormValue("order")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			_ = h.storage.Delete(r.Context(), key)
			writeValidationError(w, []fieldError{{Field: "order", Code: "invalid"}})
			return
		}
		order = parsed
	}

	photo, err := h.service.AddPhoto(r.Context(), viewer, viewer.UserID, placeID, url, photoType, order)
	if err != nil {
		// The bytes are already written; removing them keeps storage from filling with orphans
		// every time a row is rejected.
		_ = h.storage.Delete(r.Context(), key)
		if errors.Is(err, placeadmin.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Place not found.")
			return
		}
		h.logger.Error("recording a photo failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}
	writeJSON(w, http.StatusCreated, photo)
}

// removePhoto serves DELETE /v1/admin/places/{coPlace}/photos/{coPhoto}: logical, like the place
// itself, so an audit can still see what was published.
func (h *placesAdminHandlers) removePhoto(w http.ResponseWriter, r *http.Request) {
	viewer, ok := h.viewer(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	photoID := r.PathValue("coPhoto")
	if !uuidPattern.MatchString(photoID) {
		writeValidationError(w, []fieldError{{Field: "coPhoto", Code: "invalid"}})
		return
	}
	if err := h.service.RemovePhoto(r.Context(), viewer, viewer.UserID, photoID); err != nil {
		if errors.Is(err, placeadmin.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Photo not found.")
			return
		}
		h.logger.Error("retiring a photo failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
