// Package checkin records explicit presence at a place. A check-in is an append-only fact
// with an edge-validated proximity claim: the coordinate that proves it is used and then
// discarded, and only the verdict is stored (resolved Q1, D11).
package checkin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ivvanare/ecosystem_complete/api/internal/geo"
)

// Verification values mirror public.checkin_verification.
const (
	VerificationSelfReported = "SELF_REPORTED"
	VerificationProximity    = "PROXIMITY"
)

// ErrNotNearPlace is the honest answer to "you are not there". It is deliberately not a
// validation error about the coordinate: the input was well formed, the claim was false.
var ErrNotNearPlace = errors.New("the coordinate is not within the place radius")

// ErrPlaceNotFound covers soft-deleted, INACTIVE and missing alike, so a caller cannot
// enumerate retired places through error differences.
var ErrPlaceNotFound = errors.New("place not found")

// ErrAlreadyCheckedIn is the day-bucket uniqueness surfaced from the schema, not from a
// pre-check that would race.
var ErrAlreadyCheckedIn = errors.New("already checked in today")

// Checkin is the stored fact as the API returns it.
type Checkin struct {
	ID           string
	PlaceID      string
	Verification string
	CreatedAt    time.Time
}

// Location is what the place contributes to the decision: where it is and how close is
// close enough. No coordinate from the request is ever stored next to it.
type Location struct {
	Latitude  float64
	Longitude float64
	Radius    int
}

// Store is the persistence seam.
type Store interface {
	PlaceLocation(ctx context.Context, placeID string) (Location, error)
	Insert(ctx context.Context, userID, placeID, verification string, at time.Time) (Checkin, error)
}

// Service decides whether a check-in is a proximity one.
type Service struct {
	store  Store
	logger *slog.Logger
	// radius is the default tolerance when the place does not define one: the same
	// default the discovery query uses, so "near me" and "I am here" agree.
	radius int
}

// NewService wires the domain.
func NewService(store Store, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{store: store, logger: logger, radius: geo.DefaultRadius}
}

// Create records a check-in.
//
// The coordinate is required and validated: an absent or malformed one is a validation
// error the transport reports. When the claim is outside the radius the answer is
// ErrNotNearPlace and nothing is written — recording a check-in the person did not make
// would poison the signal the highlight depends on (S4).
func (s *Service) Create(ctx context.Context, userID, placeID string, where geo.Coordinate, now time.Time) (Checkin, error) {
	location, err := s.store.PlaceLocation(ctx, placeID)
	if err != nil {
		return Checkin{}, err
	}

	tolerance := location.Radius
	if tolerance <= 0 {
		tolerance = s.radius
	}
	if !WithinRadius(location, where, tolerance) {
		return Checkin{}, ErrNotNearPlace
	}

	stored, err := s.store.Insert(ctx, userID, placeID, VerificationProximity, now.UTC())
	if err != nil {
		return Checkin{}, err
	}
	return stored, nil
}

// WithinRadius reports whether the coordinate falls inside the place's circle. It uses the
// same haversine shape the discovery prefilter approximates, so the two agree at the edge
// where a person is standing on the boundary.
func WithinRadius(place Location, point geo.Coordinate, radius int) bool {
	return DistanceMeters(place.Latitude, place.Longitude, point.Lat, point.Lng) <= float64(radius)
}

// earthRadiusMeters is the mean radius used by the haversine formula.
const earthRadiusMeters = 6371000.0

// DistanceMeters returns the great-circle distance between two points.
func DistanceMeters(lat1, lng1, lat2, lng2 float64) float64 {
	rad := func(degrees float64) float64 { return degrees * math.Pi / 180 }
	dLat := rad(lat2 - lat1)
	dLng := rad(lng2 - lng1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(rad(lat1))*math.Cos(rad(lat2))*math.Sin(dLng/2)*math.Sin(dLng/2)
	if a > 1 {
		a = 1
	}
	return 2 * earthRadiusMeters * math.Asin(math.Sqrt(a))
}

// isUniqueViolation reports whether the error is a unique-constraint violation, which the
// day-bucket index surfaces as 23505.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// PostgresStore is the pgx implementation.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore wires the store.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

// PlaceLocation reads the place's coordinates and radius, refusing a place that is not
// live: a check-in to a retired place is a 404, not a fact.
func (s *PostgresStore) PlaceLocation(ctx context.Context, placeID string) (Location, error) {
	const query = `
SELECT p.nu_latitude::float8, p.nu_longitude::float8, 0
FROM core.tb_place p
WHERE p.co_place = $1
  AND p.dt_deleted_at IS NULL
  AND p.tx_status = 'ACTIVE'`
	var location Location
	if err := s.pool.QueryRow(ctx, query, placeID).Scan(&location.Latitude, &location.Longitude, &location.Radius); err != nil {
		return Location{}, fmt.Errorf("%w: %v", ErrPlaceNotFound, err)
	}
	return location, nil
}

// Insert writes the fact. The unique index over (co_user, co_place, dt_day) is the authority
// on "once per day", so this maps its violation instead of checking first.
func (s *PostgresStore) Insert(ctx context.Context, userID, placeID, verification string, at time.Time) (Checkin, error) {
	const query = `
INSERT INTO core.tb_checkin (co_user, co_place, tx_verification, dt_created_at)
VALUES ($1, $2, $3::public.checkin_verification, $4)
RETURNING co_checkin::text, co_place::text, tx_verification::text, dt_created_at`
	var stored Checkin
	err := s.pool.QueryRow(ctx, query, userID, placeID, verification, at).
		Scan(&stored.ID, &stored.PlaceID, &stored.Verification, &stored.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return Checkin{}, ErrAlreadyCheckedIn
		}
		return Checkin{}, fmt.Errorf("inserting check-in: %w", err)
	}
	return stored, nil
}
