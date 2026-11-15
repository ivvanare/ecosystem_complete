package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/ivvanare/ecosystem_complete/api/internal/feed"
)

// FeedReader is what the people the caller follows have been doing.
type FeedReader interface {
	Page(ctx context.Context, viewer, cursor string, limit int) (feed.Page, error)
}

type feedHandlers struct {
	reader FeedReader
	logger *slog.Logger
}

// list serves GET /v1/feed: reviews, check-ins and public plans of the accounts the caller
// follows, newest first, cursor-paginated.
//
// It is authenticated because it is the one read where the social graph has to be applied — a
// block filters in both directions here, which the public profile and the by-author review list
// cannot do because they do not know who is asking.
func (h *feedHandlers) list(w http.ResponseWriter, r *http.Request) {
	viewer, ok := userIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}

	limit := feed.DefaultLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeValidationError(w, []fieldError{{Field: "limit", Code: "invalid"}})
			return
		}
		limit = parsed
	}

	page, err := h.reader.Page(r.Context(), viewer, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		if errors.Is(err, feed.ErrInvalidCursor) {
			// Never a silent restart at the top: a reader who scrolls and gets page one again
			// has been shown a client bug as content.
			writeValidationError(w, []fieldError{{Field: "cursor", Code: "invalid"}})
			return
		}
		h.logger.Error("reading the feed failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}

	data := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		row := map[string]any{
			"kind":  item.Kind,
			"at":    item.At,
			"actor": map[string]any{"id": item.Actor.ID, "name": item.Actor.Name, "avatarUrl": item.Actor.AvatarURL},
		}
		if item.Place != nil {
			row["place"] = map[string]any{"id": item.Place.ID, "title": item.Place.Title}
		}
		switch item.Kind {
		case feed.KindReview:
			row["coReview"] = item.CoReview
			row["rating"] = item.Rating
			row["comment"] = item.Comment
		case feed.KindCheckin:
			row["coCheckin"] = item.CoCheckin
		case feed.KindPlan:
			if item.Plan != nil {
				row["plan"] = map[string]any{
					"coRoute":     item.Plan.ID,
					"name":        item.Plan.Name,
					"visibility":  item.Plan.Visibility,
					"scheduledAt": item.Plan.ScheduledAt,
				}
			}
		}
		data = append(data, row)
	}

	var next *string
	if page.Next != "" {
		next = &page.Next
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "nextCursor": next})
}
