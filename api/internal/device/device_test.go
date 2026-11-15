package device

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeStore struct {
	device Device
	err    error
	// What the store received, so the rules the service claims to apply can be observed.
	token    string
	platform string
	owner    string
}

func (f *fakeStore) Register(_ context.Context, userID, token, platform string) (Device, error) {
	f.owner, f.token, f.platform = userID, token, platform
	return f.device, f.err
}

func (f *fakeStore) Unregister(_ context.Context, userID, deviceID string) error {
	f.owner, f.token = userID, deviceID
	return f.err
}

// A provider hands the token over with surrounding whitespace more often than anyone expects,
// and a token with a trailing space is a device that never receives anything.
func TestTheTokenAndPlatformAreNormalizedBeforeTheStore(t *testing.T) {
	store := &fakeStore{device: Device{
		ID: "d1", Platform: PlatformAndroid, LastSeenAt: time.Now(),
	}}
	if _, err := New(store).Register(context.Background(), "u1", "  token-123\n", " android "); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if store.token != "token-123" {
		t.Errorf("token = %q, want it trimmed", store.token)
	}
	if store.platform != PlatformAndroid {
		t.Errorf("platform = %q, want it uppercased", store.platform)
	}
	if store.owner != "u1" {
		t.Errorf("owner = %q", store.owner)
	}
}

// The rules that do not need the database run before it: an empty token never reaches SQL.
func TestInvalidRegistrationsAreRejectedBeforeTheStore(t *testing.T) {
	cases := []struct {
		name     string
		token    string
		platform string
		field    string
		code     string
	}{
		{"empty token", "", PlatformAndroid, "token", "required"},
		{"blank token", "   ", PlatformAndroid, "token", "required"},
		{"missing platform", "token-1", "", "platform", "required"},
		{"unknown platform", "token-1", "WINDOWS", "platform", "invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{}
			_, err := New(store).Register(context.Background(), "u1", tc.token, tc.platform)

			var invalid ErrInvalid
			if !errors.As(err, &invalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
			if invalid.Field != tc.field || invalid.Code != tc.code {
				t.Fatalf("invalid = %+v, want {%s %s}", invalid, tc.field, tc.code)
			}
			if store.owner != "" {
				t.Fatal("the store was reached with an invalid registration")
			}
		})
	}
}

// A token longer than any provider issues is a client bug, not a registration.
func TestAnOversizedTokenIsRejected(t *testing.T) {
	long := make([]byte, MaxTokenLength+1)
	for i := range long {
		long[i] = 'a'
	}
	_, err := New(&fakeStore{}).Register(context.Background(), "u1", string(long), PlatformIOS)
	var invalid ErrInvalid
	if !errors.As(err, &invalid) || invalid.Field != "token" || invalid.Code != "too_long" {
		t.Fatalf("err = %v, want token too_long", err)
	}
}

// Removal is scoped to the owner: a 404 is the same answer for absent, already removed, and
// someone else's device, so a client cannot probe identifiers.
func TestUnregisteringAnUnknownDeviceIsNotFound(t *testing.T) {
	err := New(&fakeStore{err: ErrNotFound}).Unregister(context.Background(), "u1", "d1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestUnregisteringCarriesTheOwnerAndTheDevice(t *testing.T) {
	store := &fakeStore{}
	if err := New(store).Unregister(context.Background(), "u1", "d1"); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	if store.owner != "u1" || store.token != "d1" {
		t.Fatalf("store saw owner=%q device=%q", store.owner, store.token)
	}
}
