package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/ivvanare/ecosystem_complete/api/internal/plans"
)

// fakePublisher records what it was asked to sign, and can fail on purpose.
type fakePublisher struct {
	socketID, channel string
	err               error
	calls             int
}

func (f *fakePublisher) Publish(context.Context, string, string, any) error { return nil }

func (f *fakePublisher) SubscriptionAuth(socketID, channel string) (string, error) {
	f.calls++
	f.socketID, f.channel = socketID, channel
	if f.err != nil {
		return "", f.err
	}
	return "app-key:signed", nil
}

func realtimeRouter(plansService PlanCollaboration, publisher *fakePublisher) http.Handler {
	return New(Options{
		Plans:       plansService,
		Realtime:    publisher,
		TokenParser: authedParser("user-1"),
		Authorizer:  adminScope,
		Logger:      slog.New(slog.DiscardHandler),
	})
}

const realtimeAuthPath = "/v1/realtime/auth"

func TestSubscriptionAuthSignsAChannelThePlanAllows(t *testing.T) {
	plansService := &fakePlans{}
	publisher := &fakePublisher{}
	rec := bearerDo(t, realtimeRouter(plansService, publisher), http.MethodPost, realtimeAuthPath,
		`{"socketId":"123.456","channel":"private-plan-`+adminPlaceID+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if publisher.calls != 1 || publisher.channel != "private-plan-"+adminPlaceID {
		t.Fatalf("signed = %+v", publisher)
	}
	if plansService.gotWatchPlan != adminPlaceID {
		t.Fatalf("watched plan = %q, want %q", plansService.gotWatchPlan, adminPlaceID)
	}
	if body := rec.Body.String(); !strings.Contains(body, "app-key:signed") {
		t.Fatalf("body = %s", body)
	}
}

// The subscription door must not confirm that a private plan exists to someone who may not
// watch it: the same answer every other private-plan read gives.
func TestSubscriptionAuthAnswers404ForAnInvisiblePlan(t *testing.T) {
	plansService := &fakePlans{watchErr: plans.ErrNotFound}
	publisher := &fakePublisher{}
	rec := bearerDo(t, realtimeRouter(plansService, publisher), http.MethodPost, realtimeAuthPath,
		`{"socketId":"123.456","channel":"private-plan-`+adminPlaceID+`"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
	if publisher.calls != 0 {
		t.Fatal("a signature was issued for a plan the caller may not watch")
	}
}

func TestSubscriptionAuthRejectsAChannelThatIsNotOurs(t *testing.T) {
	publisher := &fakePublisher{}
	for _, channel := range []string{"plan-" + adminPlaceID, "private-plan-nope", "presence-plan-x"} {
		rec := bearerDo(t, realtimeRouter(&fakePlans{}, publisher), http.MethodPost, realtimeAuthPath,
			`{"socketId":"123.456","channel":"`+channel+`"}`)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("channel %q: status = %d, want 422", channel, rec.Code)
		}
	}
	if publisher.calls != 0 {
		t.Fatal("a signature was issued for a channel this service does not own")
	}
}

func TestSubscriptionAuthRejectsAMalformedSocketID(t *testing.T) {
	publisher := &fakePublisher{}
	for _, socketID := range []string{"", "nope", "123", "12.34.5"} {
		rec := bearerDo(t, realtimeRouter(&fakePlans{}, publisher), http.MethodPost, realtimeAuthPath,
			`{"socketId":"`+socketID+`","channel":"private-plan-`+adminPlaceID+`"}`)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("socket %q: status = %d, want 422", socketID, rec.Code)
		}
	}
	if publisher.calls != 0 {
		t.Fatal("a signature was issued for a malformed socket id")
	}
}

func TestSubscriptionAuthRequiresABearer(t *testing.T) {
	publisher := &fakePublisher{}
	rec := do(t, realtimeRouter(&fakePlans{}, publisher), http.MethodPost, realtimeAuthPath)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if publisher.calls != 0 {
		t.Fatal("a signature was issued without authentication")
	}
}

// A signing failure is a wiring fault: it must be an error, never a 200 with an empty string.
func TestSubscriptionAuthFailsLoudlyWhenSigningFails(t *testing.T) {
	publisher := &fakePublisher{err: errors.New("no secret")}
	rec := bearerDo(t, realtimeRouter(&fakePlans{}, publisher), http.MethodPost, realtimeAuthPath,
		`{"socketId":"123.456","channel":"private-plan-`+adminPlaceID+`"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (%s)", rec.Code, rec.Body.String())
	}
}
