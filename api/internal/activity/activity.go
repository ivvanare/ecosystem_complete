// Package activity answers "what is happening right now" for the backoffice dashboard: how many
// registrations, logins and plans in the last minutes, how big the catalogue is, and a short feed of
// the most recent events.
//
// It is a read-only projection over tables that already exist. Nothing here is stored: a dashboard
// that needs its own table is a dashboard that drifts from the data it claims to show.
package activity

import (
	"context"
	"fmt"
	"time"
)

// Window is one look-back period of the counters.
type Window struct {
	// Label is the contract's name for the window: "5m", "1h", "24h".
	Label        string
	Signups      int
	Logins       int
	PlansCreated int
}

// Totals is the size of the catalogue right now.
type Totals struct {
	Users   int
	Places  int
	Plans   int
	Reviews int
}

// Recent is one row of the live feed.
type Recent struct {
	// Kind is "signup", "login" or "plan".
	Kind string
	At   time.Time
}

// Snapshot is the whole answer.
type Snapshot struct {
	GeneratedAt time.Time
	Windows     []Window
	Totals      Totals
	Recent      []Recent
}

// Store reads the projection.
type Store interface {
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Service serves the dashboard.
type Service struct {
	store Store
}

// New wires the projection.
func New(store Store) *Service { return &Service{store: store} }

// Snapshot returns the current activity.
func (s *Service) Snapshot(ctx context.Context) (Snapshot, error) {
	snapshot, err := s.store.Snapshot(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("activity: %w", err)
	}
	if snapshot.Windows == nil {
		snapshot.Windows = []Window{}
	}
	if snapshot.Recent == nil {
		snapshot.Recent = []Recent{}
	}
	return snapshot, nil
}
