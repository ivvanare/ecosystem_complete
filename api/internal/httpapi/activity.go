package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/activity"
)

// ActivityReader is the dashboard projection.
type ActivityReader interface {
	Snapshot(ctx context.Context) (activity.Snapshot, error)
}

type activityHandlers struct {
	service ActivityReader
	logger  *slog.Logger
}

type activityWindowResponse struct {
	Window       string `json:"window"`
	Signups      int    `json:"signups"`
	Logins       int    `json:"logins"`
	PlansCreated int    `json:"plansCreated"`
}

type activityTotalsResponse struct {
	Users   int `json:"users"`
	Places  int `json:"places"`
	Plans   int `json:"plans"`
	Reviews int `json:"reviews"`
}

type activityRecentResponse struct {
	Kind string    `json:"kind"`
	At   time.Time `json:"at"`
}

type activityResponse struct {
	GeneratedAt time.Time                `json:"generatedAt"`
	Windows     []activityWindowResponse `json:"windows"`
	Totals      activityTotalsResponse   `json:"totals"`
	Recent      []activityRecentResponse `json:"recent"`
}

// activity serves GET /v1/admin/activity: what is happening right now.
//
// It exists so the panel does not build its own counters from three endpoints and disagree with
// itself: the answer comes from the tables the product already writes, live rows only.
func (h *activityHandlers) activity(w http.ResponseWriter, r *http.Request) {
	snapshot, err := h.service.Snapshot(r.Context())
	if err != nil {
		h.logger.Error("reading the activity snapshot failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return
	}

	windows := make([]activityWindowResponse, 0, len(snapshot.Windows))
	for _, window := range snapshot.Windows {
		windows = append(windows, activityWindowResponse{
			Window: window.Label, Signups: window.Signups,
			Logins: window.Logins, PlansCreated: window.PlansCreated,
		})
	}
	recent := make([]activityRecentResponse, 0, len(snapshot.Recent))
	for _, row := range snapshot.Recent {
		recent = append(recent, activityRecentResponse{Kind: row.Kind, At: row.At})
	}

	writeJSON(w, http.StatusOK, activityResponse{
		GeneratedAt: snapshot.GeneratedAt,
		Windows:     windows,
		Totals: activityTotalsResponse{
			Users: snapshot.Totals.Users, Places: snapshot.Totals.Places,
			Plans: snapshot.Totals.Plans, Reviews: snapshot.Totals.Reviews,
		},
		Recent: recent,
	})
}
