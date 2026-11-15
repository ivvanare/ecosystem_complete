package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/ivvanare/ecosystem_complete/api/internal/places"
)

type fakePlaceDetail struct {
	detail places.Detail
	err    error
	seen   string
}

func (f *fakePlaceDetail) Detail(_ context.Context, placeID string) (places.Detail, error) {
	f.seen = placeID
	return f.detail, f.err
}

// detailRouter wires only the detail read: it proves the route needs no authorizer, no token
// parser and no identity reader to answer, because it is public.
func detailRouter(reader PlaceDetailReader) http.Handler {
	return New(Options{
		PlaceDetail: reader,
		Logger:      slog.New(slog.DiscardHandler),
	})
}

const visiblePlaceID = "a0000001-0000-4000-8000-000000000001"

func sampleDetail() places.Detail {
	description := "Arepas de la esquina"
	region := "VE-A"
	open := true
	return places.Detail{
		ID: visiblePlaceID, Name: "Arepera Chacao", Description: &description,
		Category: places.CategoryRef{ID: "cat-1", Name: "Arepera"},
		Country:  "VE", Region: &region, Latitude: 10.496, Longitude: -66.853,
		Address:     nil,
		SocialMedia: map[string]string{"instagram": "https://instagram.com/arepera"},
		Timezone:    "America/Caracas", OpenNow: &open,
		Photos:     []places.Photo{{ID: "ph-1", URL: "/media/places/a.jpg", Type: "COVER", Order: 0}},
		Schedules:  []places.ScheduleDay{{DayOfWeek: 1, Open: "08:00", Close: "23:30"}},
		Menus:      []places.Menu{{Name: "Almuerzos", Items: []places.MenuItem{{Name: "Reina", Price: 3.5, Currency: "USD"}}}},
		Attributes: []places.AttributeRef{{Code: "terraza", Name: "Terraza"}},
		Rating:     &places.Stats{Average: 3.2, Count: 5},
	}
}

// The route is public: no bearer, no authorizer, and the whole body comes back.
func TestThePlaceDetailIsPublicAndComplete(t *testing.T) {
	reader := &fakePlaceDetail{detail: sampleDetail()}
	rec := do(t, detailRouter(reader), http.MethodGet, "/v1/places/"+visiblePlaceID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`"coPlace":"` + visiblePlaceID + `"`,
		`"name":"Arepera Chacao"`,
		`"category":{"id":"cat-1","name":"Arepera"}`,
		`"timezone":"America/Caracas"`,
		`"openNow":true`,
		`"type":"COVER"`,
		`"dayOfWeek":1,"open":"08:00","close":"23:30"`,
		`"price":3.5,"currency":"USD"`,
		`"code":"terraza","name":"Terraza"`,
		`"rating":{"average":3.2,"count":5}`,
		`"socialMedia":{"instagram":"https://instagram.com/arepera"}`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body = %s\nwant it to contain %s", body, want)
		}
	}
	if reader.seen != visiblePlaceID {
		t.Fatalf("the store was asked for %q", reader.seen)
	}
}

// Deleted, inactive and absent all answer the same 404, so a response never confirms that a
// place exists.
func TestAnInvisiblePlaceIs404(t *testing.T) {
	rec := do(t, detailRouter(&fakePlaceDetail{err: places.ErrNotFound}),
		http.MethodGet, "/v1/places/"+visiblePlaceID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"not_found"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

// A malformed identifier is a 404, not a 422: to a client, a broken uuid and a missing place
// are the same fact, and the store must not be reached.
func TestAMalformedIdentifierIs404AndSkipsTheStore(t *testing.T) {
	reader := &fakePlaceDetail{detail: sampleDetail()}
	rec := do(t, detailRouter(reader), http.MethodGet, "/v1/places/not-a-uuid")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if reader.seen != "" {
		t.Fatalf("the store was reached with %q", reader.seen)
	}
}

func TestAFailingDetailIsA500(t *testing.T) {
	rec := do(t, detailRouter(&fakePlaceDetail{err: errors.New("db down")}),
		http.MethodGet, "/v1/places/"+visiblePlaceID)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

// Absence is not a value: a place nobody rated and a place with no hours must not be rendered
// as zero stars or as closed.
func TestUnknownRatingAndHoursAreNull(t *testing.T) {
	detail := sampleDetail()
	detail.Rating = nil
	detail.OpenNow = nil
	detail.Photos = nil
	detail.Menus = nil
	detail.Attributes = nil
	detail.Schedules = nil

	rec := do(t, detailRouter(&fakePlaceDetail{detail: detail}),
		http.MethodGet, "/v1/places/"+visiblePlaceID)
	body := rec.Body.String()
	if !strings.Contains(body, `"rating":null`) || !strings.Contains(body, `"openNow":null`) {
		t.Fatalf("body = %s, want null rating and openNow", body)
	}
	// The service normalizes, but the transport must not leak null arrays either.
	for _, want := range []string{`"photos":[]`, `"schedules":[]`, `"menus":[]`, `"attributes":[]`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body = %s, want it to contain %s", body, want)
		}
	}
}
