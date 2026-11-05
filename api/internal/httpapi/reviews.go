package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ivvanare/ecosystem_complete/api/internal/reviews"
)

// ReviewModeratePermission opens review moderation: the capability the seed already
// grants (06-seeds.sql). Moderation invents no new permission (S12).
const ReviewModeratePermission = "review.moderate"

// commentMax is the edge bound of a review comment (p1-discovery.md §6): enforced
// here, not in the database.
const commentMax = 2000

// ReviewService is the reviews write-path surface (p1-discovery.md §6).
type ReviewService interface {
	List(ctx context.Context, placeID string, viewer reviews.Viewer, page, limit int) (reviews.ListResult, error)
	Create(ctx context.Context, actorID, placeID string, in reviews.CreateInput) (reviews.Review, error)
	Update(ctx context.Context, actorID, placeID, reviewID string, in reviews.UpdateInput) (reviews.Review, error)
	Delete(ctx context.Context, actorID, placeID, reviewID string) error
	Moderate(ctx context.Context, actorID, reviewID, status string) (reviews.Review, error)
	ListAll(ctx context.Context, filter reviews.AdminFilter, page, limit int) (reviews.ListResult, error)
}

type reviewsHandlers struct {
	service    ReviewService
	parser     TokenParser
	authorizer Authorizer
	logger     *slog.Logger
}

// reviewUpsertRequest is the create/edit body. rating is a pointer so "absent" is
// distinguishable from a value.
type reviewUpsertRequest struct {
	Rating  *int    `json:"rating"`
	Comment *string `json:"comment"`
}

// decodeReviewBody applies the strict decoding of the other endpoints. The budget is
// wider than the auth bodies because a comment carries up to 2000 characters, which
// UTF-8 encodes to more than 8 KiB.
func decodeReviewBody(w http.ResponseWriter, r *http.Request, body *reviewUpsertRequest) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body is not valid JSON.")
		return false
	}
	return true
}

// placeParam validates the {coPlace} path value at the edge: a malformed uuid is a
// validated 422, never a database error.
func placeParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	placeID := r.PathValue("coPlace")
	if !uuidPattern.MatchString(placeID) {
		writeValidationError(w, []fieldError{{Field: "coPlace", Code: "invalid"}})
		return "", false
	}
	return placeID, true
}

// reviewParam validates the {coReview} path value.
func reviewParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	reviewID := r.PathValue("coReview")
	if !uuidPattern.MatchString(reviewID) {
		writeValidationError(w, []fieldError{{Field: "coReview", Code: "invalid"}})
		return "", false
	}
	return reviewID, true
}

func validRating(rating int) bool {
	return rating >= 1 && rating <= 5
}

// validateComment enforces the 2000-character bound at the edge, counting characters
// rather than bytes: accented Spanish text is multi-byte UTF-8.
func validateComment(comment *string) *fieldError {
	if comment != nil && utf8.RuneCountInString(*comment) > commentMax {
		return &fieldError{Field: "comment", Code: "too_long"}
	}
	return nil
}

// viewer resolves who asks on the public list route (S11). A missing or invalid token
// means anonymous; the moderator flag comes from a live permission check and degrades
// closed on failure — visibility narrows, it never widens.
func (h *reviewsHandlers) viewer(r *http.Request) reviews.Viewer {
	token, ok := bearerToken(r)
	if !ok {
		return reviews.Viewer{}
	}
	claims, err := h.parser.Parse(token)
	if err != nil {
		return reviews.Viewer{}
	}
	viewer := reviews.Viewer{UserID: claims.Subject}
	if h.authorizer != nil {
		allowed, err := h.authorizer.Can(r.Context(), claims.Subject, ReviewModeratePermission)
		if err != nil {
			h.logger.Warn("review.moderate check failed, visibility narrows", slog.String("error", err.Error()))
		} else {
			viewer.Moderator = allowed
		}
	}
	return viewer
}

// list serves GET /v1/places/{coPlace}/reviews: public, paginated, newest first. The
// read never aggregates reviews for ordering (S14).
func (h *reviewsHandlers) list(w http.ResponseWriter, r *http.Request) {
	placeID, ok := placeParam(w, r)
	if !ok {
		return
	}
	page, limit := pageAndLimit(r)

	result, err := h.service.List(r.Context(), placeID, h.viewer(r), page, limit)
	if err != nil {
		h.writeServiceError(w, err, "listing reviews")
		return
	}

	data := make([]map[string]any, 0, len(result.Rows))
	for _, review := range result.Rows {
		row := map[string]any{
			"coReview": review.ID, "coPlace": review.PlaceID, "rating": review.Rating,
			"comment": review.Comment, "status": review.Status,
			"author":    map[string]any{"id": review.AuthorID, "name": review.AuthorName},
			"createdAt": review.CreatedAt, "updatedAt": review.UpdatedAt,
		}
		// The place title travels with the row: a moderator may hold review.moderate without
		// place.update and cannot resolve a uuid on its own.
		row["place"] = map[string]any{"id": review.PlaceID, "title": review.PlaceTitle}
		data = append(data, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": data, "page": result.Page, "limit": result.Limit, "total": result.Total,
	})
}

// create serves POST /v1/places/{coPlace}/reviews. The conflict is the partial unique
// index surfaced by the store: checking first would race (S10).
func (h *reviewsHandlers) create(w http.ResponseWriter, r *http.Request) {
	actorID, ok := userIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	placeID, ok := placeParam(w, r)
	if !ok {
		return
	}

	var body reviewUpsertRequest
	if !decodeReviewBody(w, r, &body) {
		return
	}

	var fields []fieldError
	if body.Rating == nil {
		fields = append(fields, fieldError{Field: "rating", Code: "required"})
	} else if !validRating(*body.Rating) {
		fields = append(fields, fieldError{Field: "rating", Code: "invalid"})
	}
	if issue := validateComment(body.Comment); issue != nil {
		fields = append(fields, *issue)
	}
	if len(fields) > 0 {
		writeValidationError(w, fields)
		return
	}

	review, err := h.service.Create(r.Context(), actorID, placeID, reviews.CreateInput{
		Rating:  *body.Rating,
		Comment: body.Comment,
	})
	if err != nil {
		h.writeServiceError(w, err, "creating review")
		return
	}
	writeJSON(w, http.StatusCreated, toReviewResponse(review))
}

// update serves PATCH /v1/places/{coPlace}/reviews/{coReview}: author-only (S11).
func (h *reviewsHandlers) update(w http.ResponseWriter, r *http.Request) {
	actorID, ok := userIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	placeID, ok := placeParam(w, r)
	if !ok {
		return
	}
	reviewID, ok := reviewParam(w, r)
	if !ok {
		return
	}

	var body reviewUpsertRequest
	if !decodeReviewBody(w, r, &body) {
		return
	}

	var fields []fieldError
	if body.Rating == nil && body.Comment == nil {
		fields = append(fields, fieldError{Field: "rating", Code: "required"})
	}
	if body.Rating != nil && !validRating(*body.Rating) {
		fields = append(fields, fieldError{Field: "rating", Code: "invalid"})
	}
	if issue := validateComment(body.Comment); issue != nil {
		fields = append(fields, *issue)
	}
	if len(fields) > 0 {
		writeValidationError(w, fields)
		return
	}

	review, err := h.service.Update(r.Context(), actorID, placeID, reviewID, reviews.UpdateInput{
		Rating:  body.Rating,
		Comment: body.Comment,
	})
	if err != nil {
		h.writeServiceError(w, err, "updating review")
		return
	}
	writeJSON(w, http.StatusOK, toReviewResponse(review))
}

// delete serves DELETE /v1/places/{coPlace}/reviews/{coReview}: author-only and
// logical, so the author may review the place again (S11).
func (h *reviewsHandlers) delete(w http.ResponseWriter, r *http.Request) {
	actorID, ok := userIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	placeID, ok := placeParam(w, r)
	if !ok {
		return
	}
	reviewID, ok := reviewParam(w, r)
	if !ok {
		return
	}

	if err := h.service.Delete(r.Context(), actorID, placeID, reviewID); err != nil {
		h.writeServiceError(w, err, "deleting review")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type moderateRequest struct {
	Status string `json:"status"`
}

// validReviewStatuses mirrors the existing review_status enum (S12).
var validReviewStatuses = map[string]bool{
	reviews.StatusActive:    true,
	reviews.StatusInactive:  true,
	reviews.StatusModerated: true,
}

// moderate serves PATCH /v1/admin/reviews/{coReview}, behind the review.moderate
// capability. It is not place-scoped: the admin route addresses the review directly.
// adminList serves GET /v1/admin/reviews: the moderator listing.
//
// The route exists because the moderation route takes a coReview and nothing could
// discover one. Authorization is the middleware's (review.moderate); this handler only
// validates the filters and maps the page.
func (h *reviewsHandlers) adminList(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	filter := reviews.AdminFilter{
		Status:   strings.TrimSpace(query.Get("status")),
		PlaceID:  strings.TrimSpace(query.Get("placeId")),
		AuthorID: strings.TrimSpace(query.Get("authorId")),
	}

	var fields []fieldError
	if filter.Status != "" && !validReviewStatuses[filter.Status] {
		fields = append(fields, fieldError{Field: "status", Code: "invalid"})
	}
	if filter.PlaceID != "" && !uuidPattern.MatchString(filter.PlaceID) {
		fields = append(fields, fieldError{Field: "placeId", Code: "invalid"})
	}
	if filter.AuthorID != "" && !uuidPattern.MatchString(filter.AuthorID) {
		fields = append(fields, fieldError{Field: "authorId", Code: "invalid"})
	}
	if len(fields) > 0 {
		writeValidationError(w, fields)
		return
	}

	page, limit := pageAndLimit(r)
	result, err := h.service.ListAll(r.Context(), filter, page, limit)
	if err != nil {
		h.writeServiceError(w, err, "listing reviews for moderation")
		return
	}

	// The moderator row carries the place title: a moderator may hold review.moderate without
	// place.update, and then a bare coPlace uuid is all the screen can show.
	data := make([]map[string]any, 0, len(result.Rows))
	for _, review := range result.Rows {
		data = append(data, map[string]any{
			"coReview": review.ID, "coPlace": review.PlaceID, "rating": review.Rating,
			"comment": review.Comment, "status": review.Status,
			"author":    map[string]any{"id": review.AuthorID, "name": review.AuthorName},
			"place":     map[string]any{"id": review.PlaceID, "title": review.PlaceTitle},
			"createdAt": review.CreatedAt, "updatedAt": review.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": data, "page": result.Page, "limit": result.Limit, "total": result.Total,
	})
}

func (h *reviewsHandlers) moderate(w http.ResponseWriter, r *http.Request) {
	actorID, ok := userIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	reviewID, ok := reviewParam(w, r)
	if !ok {
		return
	}

	var body moderateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body is not valid JSON.")
		return
	}

	var fields []fieldError
	if body.Status == "" {
		fields = append(fields, fieldError{Field: "status", Code: "required"})
	} else if !validReviewStatuses[body.Status] {
		fields = append(fields, fieldError{Field: "status", Code: "invalid"})
	}
	if len(fields) > 0 {
		writeValidationError(w, fields)
		return
	}

	review, err := h.service.Moderate(r.Context(), actorID, reviewID, body.Status)
	if err != nil {
		h.writeServiceError(w, err, "moderating review")
		return
	}
	writeJSON(w, http.StatusOK, toReviewResponse(review))
}

// writeServiceError maps the service vocabulary onto the catalogue. The only new code
// of this commit is review_already_exists; everything else is reused (S9).
func (h *reviewsHandlers) writeServiceError(w http.ResponseWriter, err error, logMessage string) {
	switch {
	case errors.Is(err, reviews.ErrAlreadyExists):
		writeError(w, http.StatusConflict, "review_already_exists", "You already have a review for this place.")
	case errors.Is(err, reviews.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Not found.")
	case errors.Is(err, reviews.ErrForbidden):
		writeError(w, http.StatusForbidden, "permission_denied", "You do not have permission to perform this action.")
	default:
		h.logger.Error(logMessage, slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}

// reviewResponse is one review as the contract documents it (openapi.yaml Review).
// The author carries display-safe fields only: the email is never exposed.
type reviewResponse struct {
	CoReview string  `json:"coReview"`
	CoPlace  string  `json:"coPlace"`
	Rating   int     `json:"rating"`
	Comment  *string `json:"comment"`
	Status   string  `json:"status"`
	Author   struct {
		ID   string  `json:"id"`
		Name *string `json:"name"`
	} `json:"author"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// ViaFollowedAuthor is the viewer-relative highlight (S5): true when the reader
	// follows this review's author. It is a property of the read, so the same review is
	// highlighted for one viewer and plain for another — never cache this under a key
	// that lacks the viewer (S18).
	ViaFollowedAuthor bool `json:"viaFollowedAuthor"`
}

func toReviewResponse(review reviews.Review) reviewResponse {
	response := reviewResponse{
		CoReview:  review.ID,
		CoPlace:   review.PlaceID,
		Rating:    review.Rating,
		Comment:   review.Comment,
		Status:    review.Status,
		CreatedAt: review.CreatedAt,
		UpdatedAt: review.UpdatedAt,

		ViaFollowedAuthor: review.ViaFollowedAuthor,
	}
	response.Author.ID = review.AuthorID
	response.Author.Name = review.AuthorName
	return response
}
