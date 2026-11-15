package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/activity"
)

type fakeActivity struct {
	snapshot activity.Snapshot
	err      error
	calls    int
}

func (f *fakeActivity) Snapshot(context.Context) (activity.Snapshot, error) {
	f.calls++
	return f.snapshot, f.err
}

func activityRouter(service ActivityReader, authorizer Authorizer) http.Handler {
	return New(Options{
		Activity:    service,
		TokenParser: authedParser("operator"),
		Authorizer:  authorizer,
		Logger:      slog.New(slog.DiscardHandler),
	})
}

func sampleSnapshot() activity.Snapshot {
	return activity.Snapshot{
		GeneratedAt: time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC),
		Windows: []activity.Window{
			{Label: "5m", Signups: 2, Logins: 7, PlansCreated: 1},
			{Label: "1h", Signups: 5, Logins: 30, PlansCreated: 4},
			{Label: "24h", Signups: 9, Logins: 120, PlansCreated: 11},
		},
		Totals: activity.Totals{Users: 42, Places: 20, Plans: 8, Reviews: 63},
		Recent: []activity.Recent{{Kind: "login", At: time.Date(2026, 11, 15, 11, 59, 0, 0, time.UTC)}},
	}
}

func TestTheActivitySnapshotCarriesTheWindowsTotalsAndFeed(t *testing.T) {
	rec := bearerDo(t, activityRouter(&fakeActivity{snapshot: sampleSnapshot()}, roleScope),
		http.MethodGet, "/v1/admin/activity", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`"windows":[{"window":"5m","signups":2,"logins":7,"plansCreated":1}`,
		`"window":"24h"`,
		`"totals":{"users":42,"places":20,"plans":8,"reviews":63}`,
		`"recent":[{"kind":"login"`,
		`"generatedAt":"2026-11-15T12:00:00Z"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body = %s\nwant it to contain %s", body, want)
		}
	}
}

// The dashboard is a backoffice read: a caller without admin.access must not see the size of the
// business or how many people logged in.
func TestTheActivitySnapshotRequiresAdminAccess(t *testing.T) {
	service := &fakeActivity{snapshot: sampleSnapshot()}
	rec := bearerDo(t, activityRouter(service, grantedPermissions{granted: map[string]bool{}}),
		http.MethodGet, "/v1/admin/activity", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if service.calls != 0 {
		t.Fatal("the projection was read without admin.access")
	}
}

func TestTheActivitySnapshotRequiresABearer(t *testing.T) {
	rec := do(t, activityRouter(&fakeActivity{snapshot: sampleSnapshot()}, roleScope),
		http.MethodGet, "/v1/admin/activity")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAFailingProjectionIsA500(t *testing.T) {
	rec := bearerDo(t, activityRouter(&fakeActivity{err: errors.New("db down")}, roleScope),
		http.MethodGet, "/v1/admin/activity", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

// An empty environment is a real state: predicates must not have to guard against nil.
func TestAnEmptyEnvironmentAnswersEmptyArrays(t *testing.T) {
	rec := bearerDo(t, activityRouter(&fakeActivity{snapshot: activity.Snapshot{}}, roleScope),
		http.MethodGet, "/v1/admin/activity", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "null") {
		t.Fatalf("body = %s, want empty arrays rather than null", rec.Body.String())
	}
}
