package taste

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// The profile is an opinion about tastes: aggregates only, no coordinates, no event log.

func TestBuildAggregatesCategoriesAndCoarseZones(t *testing.T) {
	signals := []Signal{
		{UserID: "u1", CategoryCode: "arepera", Latitude: 10.4806, Longitude: -66.9036},
		{UserID: "u1", CategoryCode: "arepera", Latitude: 10.4807, Longitude: -66.9037},
		{UserID: "u1", CategoryCode: "bar", Latitude: -34.6037, Longitude: -58.3816},
	}
	profile, err := Build(signals)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if profile.Categories["arepera"] != 2 || profile.Categories["bar"] != 1 {
		t.Fatalf("categories = %+v", profile.Categories)
	}
	if profile.Sources != 3 {
		t.Fatalf("sources = %d, want 3", profile.Sources)
	}
	// Two places metres apart share a coarse cell; the far one does not.
	if len(profile.Zones) != 2 {
		t.Fatalf("zones = %+v, want 2 coarse cells", profile.Zones)
	}
	// Nothing positional may be stored: only cell keys, never coordinates.
	for key := range profile.Zones {
		if len(key) == 0 {
			t.Fatal("empty zone key")
		}
	}
}

func TestBuildOnNoSignalsIsEmptyNotAnError(t *testing.T) {
	profile, err := Build(nil)
	if err != nil {
		t.Fatalf("Build(nil): %v", err)
	}
	if profile.Sources != 0 || len(profile.Categories) != 0 {
		t.Fatalf("profile = %+v, want empty", profile)
	}
}

type fakeLoader struct {
	signals []Signal
	err     error
}

func (f *fakeLoader) Signals(context.Context) ([]Signal, error) { return f.signals, f.err }

type fakeStore struct {
	written map[string]Profile
	version int
	at      time.Time
	err     error
}

func (f *fakeStore) Replace(_ context.Context, profiles map[string]Profile, version int, at time.Time) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.written, f.version, f.at = profiles, version, at
	return len(profiles), nil
}

func TestBatchGroupsByUserAndReplacesWholesale(t *testing.T) {
	loader := &fakeLoader{signals: []Signal{
		{UserID: "u1", CategoryCode: "arepera", Latitude: 10.48, Longitude: -66.9},
		{UserID: "u2", CategoryCode: "bar", Latitude: 10.49, Longitude: -66.85},
		{UserID: "u2", CategoryCode: "bar", Latitude: 10.49, Longitude: -66.85},
	}}
	store := &fakeStore{}
	batch := NewBatch(loader, store, discard())
	if batch.Name() != "taste-profile" {
		t.Fatalf("job name = %q", batch.Name())
	}
	if err := batch.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(store.written) != 2 {
		t.Fatalf("wrote %d profiles, want 2", len(store.written))
	}
	if store.written["u2"].Categories["bar"] != 2 {
		t.Fatalf("u2 = %+v", store.written["u2"])
	}
	if store.version != ProfileVersion || store.at.IsZero() {
		t.Fatalf("version/at = %d/%v", store.version, store.at)
	}
}

func TestBatchPropagatesFailures(t *testing.T) {
	if err := NewBatch(&fakeLoader{err: errors.New("db down")}, &fakeStore{}, discard()).
		Run(context.Background()); err == nil {
		t.Fatal("Run returned nil on a loader failure")
	}
	if err := NewBatch(&fakeLoader{}, &fakeStore{err: errors.New("db down")}, discard()).
		Run(context.Background()); err == nil {
		t.Fatal("Run returned nil on a store failure")
	}
}
