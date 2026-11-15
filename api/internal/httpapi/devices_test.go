package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/device"
)

// fakeDeviceStore is the persistence seam only. The router is wired with the REAL service on
// purpose: the validation rule and its mapping to 422 are what these tests are about, and a
// fake registry would have skipped both.
type fakeDeviceStore struct {
	registered device.Device
	err        error
	token      string
	platform   string
	removed    string
}

func (f *fakeDeviceStore) Register(_ context.Context, _, token, platform string) (device.Device, error) {
	f.token, f.platform = token, platform
	return f.registered, f.err
}

func (f *fakeDeviceStore) Unregister(_ context.Context, _, deviceID string) error {
	f.removed = deviceID
	return f.err
}

func deviceRouter(store *fakeDeviceStore) http.Handler {
	return New(Options{
		Devices:     device.New(store),
		TokenParser: authedParser("u1"),
		Logger:      slog.New(slog.DiscardHandler),
	})
}

const deviceID = "0b7dc2b3-2f4a-4c2f-9a1c-6c3a1f0f5d21"

// Registering is authenticated and answers 201 with the identifier the client needs to remove
// the device later.
func TestRegisteringADeviceIs201WithItsId(t *testing.T) {
	registry := &fakeDeviceStore{registered: device.Device{
		ID: deviceID, Platform: device.PlatformAndroid,
		LastSeenAt: time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC),
	}}
	rec := bearerDo(t, deviceRouter(registry), http.MethodPost, "/v1/devices",
		`{"token":"token-abc","platform":"ANDROID"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`"coDevice":"` + deviceID + `"`, `"platform":"ANDROID"`, `"lastSeenAt":"2026-11-15T12:00:00Z"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body = %s\nwant %s", body, want)
		}
	}
	if registry.token != "token-abc" {
		t.Fatalf("the registry saw token %q", registry.token)
	}
}

// An anonymous caller gets a 401: a push has an owner, and an unowned token is a message with
// nobody to address.
func TestRegisteringADeviceRequiresAuthentication(t *testing.T) {
	rec := do(t, deviceRouter(&fakeDeviceStore{}), http.MethodPost, "/v1/devices")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// The closed vocabulary of field errors, mapped from the domain rule instead of duplicated.
func TestAnInvalidRegistrationAnswers422WithTheField(t *testing.T) {
	cases := []struct{ body, field, code string }{
		{`{"platform":"ANDROID"}`, "token", "required"},
		{`{"token":"t"}`, "platform", "required"},
		{`{"token":"t","platform":"WINDOWS"}`, "platform", "invalid"},
	}
	for _, tc := range cases {
		rec := bearerDo(t, deviceRouter(&fakeDeviceStore{}), http.MethodPost, "/v1/devices", tc.body)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("body %s → status %d, want 422", tc.body, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"field":"`+tc.field+`","code":"`+tc.code+`"`) {
			t.Fatalf("body = %s, want field %s code %s", rec.Body.String(), tc.field, tc.code)
		}
	}
}

func TestUnregisteringADeviceIs204(t *testing.T) {
	registry := &fakeDeviceStore{}
	rec := bearerDo(t, deviceRouter(registry), http.MethodDelete, "/v1/devices/"+deviceID, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if registry.removed != deviceID {
		t.Fatalf("the registry removed %q", registry.removed)
	}
}

// Absent, already removed and someone else's are one answer, so a client cannot probe.
func TestUnregisteringAnUnknownDeviceIs404(t *testing.T) {
	rec := bearerDo(t, deviceRouter(&fakeDeviceStore{err: device.ErrNotFound}),
		http.MethodDelete, "/v1/devices/"+deviceID, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestUnregisteringAMalformedIdIs404WithoutReachingTheRegistry(t *testing.T) {
	registry := &fakeDeviceStore{}
	rec := bearerDo(t, deviceRouter(registry), http.MethodDelete, "/v1/devices/nope", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if registry.removed != "" {
		t.Fatalf("the registry was reached with %q", registry.removed)
	}
}

func TestAFailingRegistrationIsA500(t *testing.T) {
	rec := bearerDo(t, deviceRouter(&fakeDeviceStore{err: errors.New("db down")}),
		http.MethodPost, "/v1/devices", `{"token":"t","platform":"IOS"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
