// Package security holds the primitives the auth flows share.
package security

import (
	"fmt"
	"time"
)

// TooManyRequestsError means the caller exhausted a quota. Callers translate it into a 429
// with a Retry-After header, the same shape the login lockout uses.
type TooManyRequestsError struct {
	RetryAfter time.Duration
}

func (e *TooManyRequestsError) Error() string {
	return fmt.Sprintf("security: quota exhausted, retry in %s", e.RetryAfter.Round(time.Second))
}

// Quota bounds how often a code can be requested.
//
// A per-token attempt counter is not enough on its own: if the token can be reissued at will,
// every reissue resets the counter and a 6-digit code becomes brute-forceable again. The
// quota is what actually bounds the guess space, and it also stops the endpoint from being
// used to flood somebody else's inbox.
type Quota struct {
	// Cooldown is the minimum gap between two requests.
	Cooldown time.Duration
	// Window and Limit cap the requests inside a rolling window.
	Window time.Duration
	Limit  int
}

// Defaults shared by the flows that mail a code.
const (
	DefaultCooldown = 60 * time.Second
	DefaultWindow   = 24 * time.Hour
	DefaultLimit    = 5
)

// Check reports whether a new request is allowed, given the last request time and how many
// requests happened inside the window.
//
// A nil lastAt means no request was ever made.
func (q Quota) Check(now time.Time, lastAt *time.Time, inWindow int) error {
	cooldown := q.Cooldown
	if cooldown <= 0 {
		cooldown = DefaultCooldown
	}
	window := q.Window
	if window <= 0 {
		window = DefaultWindow
	}
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}

	if lastAt != nil {
		if elapsed := now.Sub(*lastAt); elapsed < cooldown {
			return &TooManyRequestsError{RetryAfter: cooldown - elapsed}
		}
	}

	if inWindow >= limit {
		// The exact instant the oldest request leaves the window is not tracked; the whole
		// window is a conservative answer that never tells the caller to retry too early.
		return &TooManyRequestsError{RetryAfter: window}
	}

	return nil
}
