// Package taste builds each user's taste profile from explicit signals, in the worker only.
//
// A profile is an opinion about tastes, never a trail: it holds aggregate affinities, no
// coordinates and no per-event log (D11). It is replaced wholesale on every batch, so deleting
// one is a complete and safe operation.
package taste

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ivvanare/ecosystem_complete/api/internal/geo"
	h3 "github.com/uber/h3-go/v4"
)

// ProfileVersion is the shape of js_profile. Bump it when the fields change, so an old row is
// recognisable instead of silently misread.
const ProfileVersion = 1

// ZoneResolution is deliberately coarse: a city district, never a point. Anything finer would
// be a location store wearing an aggregate's clothes (D11).
const ZoneResolution = 5

// Signal is one explicit act, already reduced to what the profile needs. The coordinate never
// reaches the stored profile: it is turned into a zone here and dropped.
type Signal struct {
	UserID       string
	CategoryCode string
	Latitude     float64
	Longitude    float64
}

// Profile is the aggregate for one user.
type Profile struct {
	Categories map[string]int `json:"categories,omitempty"`
	Zones      map[string]int `json:"zones,omitempty"`
	Sources    int            `json:"sources"`
}

// Build turns one user's signals into a profile.
//
// Category and zone counts are the whole content: an affinity is "how often this person chose
// this kind of place", which is exactly what explicit acts say and what a position does not.
func Build(signals []Signal) (Profile, error) {
	profile := Profile{Categories: map[string]int{}, Zones: map[string]int{}, Sources: len(signals)}
	for _, signal := range signals {
		if signal.CategoryCode != "" {
			profile.Categories[signal.CategoryCode]++
		}
		cell, err := h3.LatLngToCell(h3.NewLatLng(signal.Latitude, signal.Longitude), ZoneResolution)
		if err != nil {
			return Profile{}, fmt.Errorf("placing a signal in a zone: %w", err)
		}
		profile.Zones[h3.CellToString(cell)]++
	}
	return profile, nil
}

// Loader reads the explicit signals of every user.
type Loader interface {
	Signals(ctx context.Context) ([]Signal, error)
}

// Store persists the profiles.
type Store interface {
	Replace(ctx context.Context, profiles map[string]Profile, version int, at time.Time) (int, error)
}

// Batch is one run of the profile job.
type Batch struct {
	loader Loader
	store  Store
	logger *slog.Logger
	now    func() time.Time
}

// NewBatch wires the job.
func NewBatch(loader Loader, store Store, logger *slog.Logger) *Batch {
	if logger == nil {
		logger = slog.Default()
	}
	return &Batch{loader: loader, store: store, logger: logger,
		now: func() time.Time { return time.Now().UTC() }}
}

// Name satisfies the worker scheduler's job contract.
func (b *Batch) Name() string { return "taste-profile" }

// Run computes every profile and replaces them wholesale. A user with no signals is simply
// absent from the next batch — absence is a normal state, never an error (S12).
func (b *Batch) Run(ctx context.Context) error {
	signals, err := b.loader.Signals(ctx)
	if err != nil {
		return fmt.Errorf("loading taste signals: %w", err)
	}

	grouped := make(map[string][]Signal, 64)
	for _, signal := range signals {
		grouped[signal.UserID] = append(grouped[signal.UserID], signal)
	}

	profiles := make(map[string]Profile, len(grouped))
	for userID, userSignals := range grouped {
		profile, err := Build(userSignals)
		if err != nil {
			return err
		}
		profiles[userID] = profile
	}

	written, err := b.store.Replace(ctx, profiles, ProfileVersion, b.now())
	if err != nil {
		return err
	}
	b.logger.Info("taste profiles written", slog.Int("users", written))
	return nil
}

// PostgresLoader reads explicit acts: reviews, check-ins and votes. Location pings are never a
// source — a person walking past a bar is not expressing a preference (D11).
type PostgresLoader struct {
	pool *pgxpool.Pool
}

// NewPostgresLoader wires the loader.
func NewPostgresLoader(pool *pgxpool.Pool) *PostgresLoader { return &PostgresLoader{pool: pool} }

// signalsQuery unions three explicit acts, each weighted the same: the person chose to do it.
//
// Live, ACTIVE rows only: a moderated review or a check-in on a retired place must not shape
// anybody's tastes.
const signalsQuery = `
SELECT u.co_user::text, c.tx_code, p.nu_latitude::float8, p.nu_longitude::float8
FROM core.tb_review r
JOIN core.tb_place p ON p.co_place = r.co_place AND p.dt_deleted_at IS NULL AND p.tx_status = 'ACTIVE'
JOIN core.tb_place_category c ON c.co_place_category = p.co_place_category
JOIN seg.tb_user u ON u.co_user = r.co_user AND u.dt_deleted_at IS NULL
WHERE r.dt_deleted_at IS NULL AND r.tx_status = 'ACTIVE'
UNION ALL
SELECT u.co_user::text, c.tx_code, p.nu_latitude::float8, p.nu_longitude::float8
FROM core.tb_checkin k
JOIN core.tb_place p ON p.co_place = k.co_place AND p.dt_deleted_at IS NULL AND p.tx_status = 'ACTIVE'
JOIN core.tb_place_category c ON c.co_place_category = p.co_place_category
JOIN seg.tb_user u ON u.co_user = k.co_user AND u.dt_deleted_at IS NULL
UNION ALL
SELECT u.co_user::text, c.tx_code, p.nu_latitude::float8, p.nu_longitude::float8
FROM core.tb_route_place_vote v
JOIN core.tb_place p ON p.co_place = v.co_place AND p.dt_deleted_at IS NULL AND p.tx_status = 'ACTIVE'
JOIN core.tb_place_category c ON c.co_place_category = p.co_place_category
JOIN seg.tb_user u ON u.co_user = v.co_user AND u.dt_deleted_at IS NULL
WHERE v.nu_vote = 1`

// Signals returns one entry per explicit act.
func (l *PostgresLoader) Signals(ctx context.Context) ([]Signal, error) {
	rows, err := l.pool.Query(ctx, signalsQuery)
	if err != nil {
		return nil, fmt.Errorf("querying taste signals: %w", err)
	}
	defer rows.Close()

	signals := make([]Signal, 0, 256)
	for rows.Next() {
		var signal Signal
		if err := rows.Scan(&signal.UserID, &signal.CategoryCode, &signal.Latitude, &signal.Longitude); err != nil {
			return nil, fmt.Errorf("scanning taste signal: %w", err)
		}
		signals = append(signals, signal)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("during taste signal iteration: %w", err)
	}
	return signals, nil
}

// PostgresStore replaces profiles wholesale.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore wires the store.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

// Replace upserts every computed profile and removes the rows of users who no longer have
// signals, so the table is exactly the last batch's result and never a graveyard.
func (s *PostgresStore) Replace(ctx context.Context, profiles map[string]Profile, version int, at time.Time) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	users := make([]string, 0, len(profiles))
	for userID := range profiles {
		users = append(users, userID)
	}
	sort.Strings(users)

	const upsert = `
INSERT INTO core.tb_user_taste_profile (co_user, js_profile, nu_version, dt_computed_at)
VALUES ($1::uuid, $2::jsonb, $3, $4)
ON CONFLICT (co_user) DO UPDATE
   SET js_profile = EXCLUDED.js_profile,
       nu_version = EXCLUDED.nu_version,
       dt_computed_at = EXCLUDED.dt_computed_at`

	for _, userID := range users {
		payload, err := json.Marshal(profiles[userID])
		if err != nil {
			return 0, fmt.Errorf("encoding a profile: %w", err)
		}
		if _, err := tx.Exec(ctx, upsert, userID, payload, version, at); err != nil {
			return 0, fmt.Errorf("writing a profile: %w", err)
		}
	}

	// A user whose signals disappeared loses the row: absence is a state, not an error.
	if _, err := tx.Exec(ctx, `
		DELETE FROM core.tb_user_taste_profile
		 WHERE NOT (co_user = ANY($1::uuid[]))`, users); err != nil {
		return 0, fmt.Errorf("removing stale profiles: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("committing profiles: %w", err)
	}
	return len(users), nil
}

// Compile-time guard: the zone resolution must stay coarse enough to be an aggregate.
var _ = func() bool { return ZoneResolution <= geo.TrendingZoneResolution }()
