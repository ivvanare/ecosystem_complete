package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/profile"
)

type fakeProfile struct {
	account profile.Profile
	err     error
	seen    string
}

func (f *fakeProfile) Profile(_ context.Context, userID string) (profile.Profile, error) {
	f.seen = userID
	return f.account, f.err
}

// profileRouter wires only the profile read: no authorizer and no token parser, because the
// route is public.
func profileRouter(reader ProfileReader) http.Handler {
	return New(Options{
		PublicProfile: reader,
		Logger:        slog.New(slog.DiscardHandler),
	})
}

const publicUserID = "695b5086-b825-46b6-b072-867c73e89686"

func TestTheProfileIsPublicAndCarriesTheCounters(t *testing.T) {
	name := "Ana"
	reader := &fakeProfile{account: profile.Profile{
		ID: publicUserID, Name: &name,
		MemberSince: time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC),
		Counts:      profile.Counts{Reviews: 3, Checkins: 12, Followers: 5, Following: 8},
	}}

	rec := do(t, profileRouter(reader), http.MethodGet, "/v1/users/"+publicUserID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`"coUser":"` + publicUserID + `"`,
		`"name":"Ana"`,
		`"memberSince":"2026-01-02T10:00:00Z"`,
		`"counts":{"reviews":3,"checkins":12,"followers":5,"following":8}`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body = %s\nwant it to contain %s", body, want)
		}
	}
	// The email is a credential, not a profile field: it must never appear in this body.
	if strings.Contains(body, "email") || strings.Contains(body, "@") {
		t.Fatalf("body = %s, want no email", body)
	}
}

func TestASuspendedOrMissingAccountIs404(t *testing.T) {
	rec := do(t, profileRouter(&fakeProfile{err: profile.ErrNotFound}),
		http.MethodGet, "/v1/users/"+publicUserID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"not_found"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestAMalformedUserIdentifierIs404AndSkipsTheStore(t *testing.T) {
	reader := &fakeProfile{}
	rec := do(t, profileRouter(reader), http.MethodGet, "/v1/users/not-a-uuid")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if reader.seen != "" {
		t.Fatalf("the store was reached with %q", reader.seen)
	}
}

func TestAFailingProfileIsA500(t *testing.T) {
	rec := do(t, profileRouter(&fakeProfile{err: errors.New("db down")}),
		http.MethodGet, "/v1/users/"+publicUserID)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

// A counters block of zeroes is a real profile (a person who just signed up), not an omission.
func TestAProfileWithNoActivityStillAnswersZeroes(t *testing.T) {
	rec := do(t, profileRouter(&fakeProfile{account: profile.Profile{ID: publicUserID}}),
		http.MethodGet, "/v1/users/"+publicUserID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"counts":{"reviews":0,"checkins":0,"followers":0,"following":0}`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}
