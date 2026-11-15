// Package profile serves the public identity of another person: who they are and how much they
// have done, never how to reach them.
//
// It exists because follows, reviews and check-ins all point at a `co_user`, and until now
// tapping one of those names led nowhere. What it returns is the minimum a client needs to
// render that screen: a display name, the day the account appeared, and four counters.
package profile

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound means the account is not visible: absent, deleted or not active. A suspended or
// retired account answers exactly like one that never existed.
var ErrNotFound = errors.New("profile: not found")

// Counts is what the person has done, live rows only.
type Counts struct {
	Reviews   int
	Checkins  int
	Followers int
	Following int
}

// Profile is the public read of one account.
//
// The email is deliberately absent: it is a credential, and the review read already publishes
// only the display-safe pair. Nothing here is viewer-relative either — a cached answer must not
// depend on who asked — so there is no followedByMe flag.
type Profile struct {
	ID   string
	Name *string
	// AvatarURL is nil when the account never uploaded one: the client draws initials.
	AvatarURL   *string
	MemberSince time.Time
	Counts      Counts
}

// Store is the persistence seam.
type Store interface {
	Profile(ctx context.Context, userID string) (Profile, error)
}

// Service serves the public profile.
type Service struct {
	store Store
}

// New wires the read.
func New(store Store) *Service { return &Service{store: store} }

// Profile returns the account, or ErrNotFound when it is not publicly visible.
func (s *Service) Profile(ctx context.Context, userID string) (Profile, error) {
	profile, err := s.store.Profile(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Profile{}, ErrNotFound
		}
		return Profile{}, fmt.Errorf("profile: %w", err)
	}
	return profile, nil
}
