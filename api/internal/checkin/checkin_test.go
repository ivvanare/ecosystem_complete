package checkin

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/geo"
)

type fakeStore struct {
	location    Location
	locationErr error
	inserted    []string
	insertErr   error
}

func (f *fakeStore) PlaceLocation(context.Context, string) (Location, error) {
	return f.location, f.locationErr
}

func (f *fakeStore) Insert(_ context.Context, _, placeID, verification string, at time.Time) (Checkin, error) {
	if f.insertErr != nil {
		return Checkin{}, f.insertErr
	}
	f.inserted = append(f.inserted, verification)
	return Checkin{ID: "c1", PlaceID: placeID, Verification: verification, CreatedAt: at}, nil
}

func service(store Store) *Service { return NewService(store, slog.New(slog.DiscardHandler)) }

// The place used below sits in Chacao: 10.4960, -66.8530.
var chacao = Location{Latitude: 10.4960, Longitude: -66.8530}

func TestDistanceMetersMatchesAKnownSeparation(t *testing.T) {
	// One degree of latitude is ~111.2 km; 0.01 degree is ~1.112 km.
	got := DistanceMeters(10.0, -66.0, 10.01, -66.0)
	if got < 1100 || got > 1130 {
		t.Fatalf("DistanceMeters = %.1f m for 0.01 degrees, want ~1112", got)
	}
	if d := DistanceMeters(10.0, -66.0, 10.0, -66.0); d != 0 {
		t.Fatalf("distance to itself = %v, want 0", d)
	}
}

func TestCreateInsideRadiusStoresTheVerdictOnly(t *testing.T) {
	store := &fakeStore{location: chacao}
	// ~100 m away: inside the 2000 m default.
	where := geo.Coordinate{Lat: 10.4969, Lng: -66.8530}

	stored, err := service(store).Create(context.Background(), "u1", "p1", where, time.Now())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if stored.Verification != VerificationProximity {
		t.Fatalf("verification = %q, want PROXIMITY", stored.Verification)
	}
	if len(store.inserted) != 1 || store.inserted[0] != VerificationProximity {
		t.Fatalf("store inserted %v, want exactly [PROXIMITY]", store.inserted)
	}
}

func TestCreateOutsideRadiusWritesNothing(t *testing.T) {
	store := &fakeStore{location: chacao}
	// ~11 km away: same city, wrong neighbourhood.
	where := geo.Coordinate{Lat: 10.5960, Lng: -66.8530}

	_, err := service(store).Create(context.Background(), "u1", "p1", where, time.Now())
	if !errors.Is(err, ErrNotNearPlace) {
		t.Fatalf("Create outside the radius = %v, want ErrNotNearPlace", err)
	}
	// The honest refusal is the point: a fabricated check-in would poison the highlight.
	if len(store.inserted) != 0 {
		t.Fatalf("store inserted %v, want nothing written", store.inserted)
	}
}

func TestCreatePropagatesMissingPlace(t *testing.T) {
	store := &fakeStore{locationErr: ErrPlaceNotFound}
	if _, err := service(store).Create(context.Background(), "u1", "gone", geo.Coordinate{Lat: 10.5, Lng: -66.8}, time.Now()); !errors.Is(err, ErrPlaceNotFound) {
		t.Fatalf("Create on a missing place = %v, want ErrPlaceNotFound", err)
	}
}

func TestCreatePropagatesDailyDuplicate(t *testing.T) {
	store := &fakeStore{location: chacao, insertErr: ErrAlreadyCheckedIn}
	where := geo.Coordinate{Lat: 10.4960, Lng: -66.8530}
	if _, err := service(store).Create(context.Background(), "u1", "p1", where, time.Now()); !errors.Is(err, ErrAlreadyCheckedIn) {
		t.Fatalf("Create twice = %v, want ErrAlreadyCheckedIn", err)
	}
}

func TestCreateUsesThePlaceRadiusWhenItDefinesOne(t *testing.T) {
	// A venue with a tight radius: 500 m.
	store := &fakeStore{location: Location{Latitude: 10.4960, Longitude: -66.8530, Radius: 500}}
	// ~700 m away.
	where := geo.Coordinate{Lat: 10.5023, Lng: -66.8530}
	if _, err := service(store).Create(context.Background(), "u1", "p1", where, time.Now()); !errors.Is(err, ErrNotNearPlace) {
		t.Fatalf("Create at 700 m with a 500 m radius = %v, want ErrNotNearPlace", err)
	}
}
