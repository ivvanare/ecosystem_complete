// Package device owns where a push can be delivered: which devices of which user can be
// reached right now.
//
// It is not a session. A registration token survives logins, rotates without being asked, and
// belongs to the device rather than to the person using it — which is why registering a token
// that another account already holds reassigns it instead of failing.
package device

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrNotFound means the device is unknown to this user. It is deliberately one answer for
// "does not exist", "already unregistered" and "belongs to someone else": a client must not be
// able to probe which identifiers exist.
var ErrNotFound = errors.New("device: not found")

// Platforms the provider can route to. A closed set, validated at the edge: an unknown platform
// would be a row the sender cannot deliver to.
const (
	PlatformAndroid = "ANDROID"
	PlatformIOS     = "IOS"
)

// Platforms is the accepted set.
var Platforms = []string{PlatformAndroid, PlatformIOS}

// MaxTokenLength bounds what a client may submit. FCM tokens run to roughly 160 characters and
// APNs to 64 bytes of hex; anything far beyond that is not a token.
const MaxTokenLength = 4096

// Device is one registered device.
type Device struct {
	ID         string
	Platform   string
	LastSeenAt time.Time
}

// Store is the persistence seam.
type Store interface {
	// Register upserts by token: the same phone re-registering is one row, not two.
	Register(ctx context.Context, userID, token, platform string) (Device, error)
	// Unregister removes the device only if it belongs to the user; otherwise ErrNotFound.
	Unregister(ctx context.Context, userID, deviceID string) error
}

// Service owns the rules.
type Service struct {
	store Store
}

// New wires the service.
func New(store Store) *Service { return &Service{store: store} }

// Register binds a token to the caller.
//
// The token is trimmed because providers hand it over with whitespace more often than anyone
// expects, and a token with a trailing space is a device that never receives anything.
func (s *Service) Register(ctx context.Context, userID, token, platform string) (Device, error) {
	token = strings.TrimSpace(token)
	platform = strings.ToUpper(strings.TrimSpace(platform))

	if err := Validate(token, platform); err != nil {
		return Device{}, err
	}
	device, err := s.store.Register(ctx, userID, token, platform)
	if err != nil {
		return Device{}, fmt.Errorf("device: %w", err)
	}
	return device, nil
}

// Unregister removes one of the caller's devices.
func (s *Service) Unregister(ctx context.Context, userID, deviceID string) error {
	if err := s.store.Unregister(ctx, userID, deviceID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("device: %w", err)
	}
	return nil
}

// ErrInvalid names the field that failed validation, so the transport can answer 422 without
// duplicating the rule.
type ErrInvalid struct{ Field, Code string }

func (e ErrInvalid) Error() string { return "device: " + e.Field + " " + e.Code }

// Validate applies the rules that do not need the database.
func Validate(token, platform string) error {
	if token == "" {
		return ErrInvalid{Field: "token", Code: "required"}
	}
	if len(token) > MaxTokenLength {
		return ErrInvalid{Field: "token", Code: "too_long"}
	}
	if platform == "" {
		return ErrInvalid{Field: "platform", Code: "required"}
	}
	for _, known := range Platforms {
		if platform == known {
			return nil
		}
	}
	return ErrInvalid{Field: "platform", Code: "invalid"}
}
