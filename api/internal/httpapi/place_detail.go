package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/ivvanare/ecosystem_complete/api/internal/places"
)

// PlaceDetailReader is the public detail read.
type PlaceDetailReader interface {
	Detail(ctx context.Context, placeID string) (places.Detail, error)
}

type placeDetailHandlers struct {
	reader PlaceDetailReader
	logger *slog.Logger
}

// detailResponse is openapi.yaml PlaceDetail. The domain type carries no JSON tags so the
// contract lives in one place per layer.
type detailResponse struct {
	CoPlace     string            `json:"coPlace"`
	Name        string            `json:"name"`
	Description *string           `json:"description"`
	Category    categoryRef       `json:"category"`
	Country     string            `json:"country"`
	Region      *string           `json:"region"`
	Latitude    float64           `json:"latitude"`
	Longitude   float64           `json:"longitude"`
	Address     *string           `json:"address"`
	SocialMedia map[string]string `json:"socialMedia"`
	Timezone    string            `json:"timezone"`
	OpenNow     *bool             `json:"openNow"`
	Photos      []detailPhoto     `json:"photos"`
	Schedules   []detailSchedule  `json:"schedules"`
	Menus       []detailMenu      `json:"menus"`
	Attributes  []detailAttribute `json:"attributes"`
	Rating      *placeRating      `json:"rating"`
}

type detailPhoto struct {
	CoPhoto string `json:"coPhoto"`
	URL     string `json:"url"`
	Type    string `json:"type"`
	Order   int    `json:"order"`
}

type detailSchedule struct {
	DayOfWeek int    `json:"dayOfWeek"`
	Open      string `json:"open"`
	Close     string `json:"close"`
}

type detailMenu struct {
	Name        string           `json:"name"`
	Description *string          `json:"description"`
	Items       []detailMenuItem `json:"items"`
}

type detailMenuItem struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Price       float64 `json:"price"`
	Currency    string  `json:"currency"`
}

type detailAttribute struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// detail serves GET /v1/places/{coPlace}: the read a client needs after tapping a result.
//
// It is public, like the discovery list: an app that can list places must be able to open
// them. A malformed identifier answers 404 rather than 422 — to a client, a broken uuid and a
// place that does not exist are the same fact, and only one of them is worth a retry.
func (h *placeDetailHandlers) detail(w http.ResponseWriter, r *http.Request) {
	placeID := r.PathValue("coPlace")
	if !uuidPattern.MatchString(placeID) {
		writeError(w, http.StatusNotFound, "not_found", "Place not found.")
		return
	}

	detail, err := h.reader.Detail(r.Context(), placeID)
	if err != nil {
		if errors.Is(err, places.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Place not found.")
			return
		}
		h.logger.Error("reading a place failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}

	writeJSON(w, http.StatusOK, detailResponseOf(detail))
}

// detailResponseOf maps the domain read onto the documented body. An unpublished rating stays
// null; a place with no hours reports openNow null, never false.
func detailResponseOf(detail places.Detail) detailResponse {
	response := detailResponse{
		CoPlace:     detail.ID,
		Name:        detail.Name,
		Description: detail.Description,
		Category:    categoryRef{ID: detail.Category.ID, Name: detail.Category.Name},
		Country:     detail.Country,
		Region:      detail.Region,
		Latitude:    detail.Latitude,
		Longitude:   detail.Longitude,
		Address:     detail.Address,
		SocialMedia: detail.SocialMedia,
		Timezone:    detail.Timezone,
		OpenNow:     detail.OpenNow,
		Photos:      make([]detailPhoto, 0, len(detail.Photos)),
		Schedules:   make([]detailSchedule, 0, len(detail.Schedules)),
		Menus:       make([]detailMenu, 0, len(detail.Menus)),
		Attributes:  make([]detailAttribute, 0, len(detail.Attributes)),
	}

	for _, photo := range detail.Photos {
		response.Photos = append(response.Photos, detailPhoto{
			CoPhoto: photo.ID, URL: photo.URL, Type: photo.Type, Order: photo.Order,
		})
	}
	for _, day := range detail.Schedules {
		response.Schedules = append(response.Schedules, detailSchedule{
			DayOfWeek: day.DayOfWeek, Open: day.Open, Close: day.Close,
		})
	}
	for _, menu := range detail.Menus {
		items := make([]detailMenuItem, 0, len(menu.Items))
		for _, item := range menu.Items {
			items = append(items, detailMenuItem{
				Name: item.Name, Description: item.Description,
				Price: item.Price, Currency: item.Currency,
			})
		}
		response.Menus = append(response.Menus, detailMenu{
			Name: menu.Name, Description: menu.Description, Items: items,
		})
	}
	for _, attribute := range detail.Attributes {
		response.Attributes = append(response.Attributes, detailAttribute{
			Code: attribute.Code, Name: attribute.Name,
		})
	}
	if detail.Rating != nil {
		response.Rating = &placeRating{Average: detail.Rating.Average, Count: detail.Rating.Count}
	}
	return response
}
