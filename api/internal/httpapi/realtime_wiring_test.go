package httpapi

// Realtime is optional as a whole: the four SOKETI_* values or none. Without them cmd/api wires
// no publisher at all and the subscription route must be absent, not present-and-failing — that
// is what openapi.yaml, API.md, README.md, .env.example and docker-compose.yml all promise.

import (
	"log/slog"
	"net/http"
	"testing"

	"github.com/ivvanare/ecosystem_complete/api/internal/plans"
)

func TestTheRealtimeRouteIsAbsentWhenTheBrokerIsNotConfigured(t *testing.T) {
	// Exactly the Options cmd/api/main.go builds when cfg.SoketiURL is empty: no Realtime field.
	handler := New(Options{
		Plans:       &fakePlans{},
		TokenParser: authedParser("user-1"),
		Logger:      slog.New(slog.DiscardHandler),
	})

	authenticated := bearerDo(t, handler, http.MethodPost, realtimeAuthPath,
		`{"socketId":"123.456","channel":"private-plan-`+adminPlaceID+`"}`)
	if authenticated.Code != http.StatusNotFound {
		t.Fatalf("authenticated caller on an unconfigured broker: status = %d (%s), want 404",
			authenticated.Code, authenticated.Body.String())
	}

	anonymous := do(t, handler, http.MethodPost, realtimeAuthPath)
	if anonymous.Code != http.StatusNotFound {
		t.Fatalf("anonymous caller on an unconfigured broker: status = %d (%s), want 404",
			anonymous.Code, anonymous.Body.String())
	}
}

// The channel parser is what stands between "sign my channel" and a plan the caller may watch.
// A prefix inside the plan id, a second prefix, or a padded name must never reach the signer.
func TestTheChannelParserIsNotFooledByACraftedName(t *testing.T) {
	for _, channel := range []string{
		"private-plan-x-private-plan-" + adminPlaceID,
		"private-plan-" + adminPlaceID + "-private-plan-" + adminPlaceID,
		"private-plan-" + adminPlaceID + "/",
		"private-plan-p" + adminPlaceID,
		"private-plan-" + adminPlaceID + `\u0000`, // a JSON-escaped NUL
		"Private-plan-" + adminPlaceID,
		"presence-plan-" + adminPlaceID,
		"private-plan-" + adminPlaceID[:8],
	} {
		publisher := &fakePublisher{}
		rec := bearerDo(t, realtimeRouter(&fakePlans{}, publisher), http.MethodPost, realtimeAuthPath,
			`{"socketId":"123.456","channel":"`+channel+`"}`)
		if rec.Code != http.StatusUnprocessableEntity || publisher.calls != 0 {
			t.Fatalf("channel %q: status = %d, signed = %d, want 422 and no signature",
				channel, rec.Code, publisher.calls)
		}
	}

	// A socket id is signed verbatim, so only the shape Soketi hands out may pass.
	for _, socketID := range []string{
		"123.456:private-plan-" + adminPlaceID, "12.34.56", "-1.2", `123.456\n7`,
	} {
		publisher := &fakePublisher{}
		rec := bearerDo(t, realtimeRouter(&fakePlans{}, publisher), http.MethodPost, realtimeAuthPath,
			`{"socketId":"`+socketID+`","channel":"private-plan-`+adminPlaceID+`"}`)
		if rec.Code != http.StatusUnprocessableEntity || publisher.calls != 0 {
			t.Fatalf("socket %q: status = %d, signed = %d, want 422 and no signature",
				socketID, rec.Code, publisher.calls)
		}
	}
}

// The subscription keeps the split the plan's own door makes: visible is 200 and a signature,
// invisible is 404 and none.
func TestSubscriptionAuthKeepsTheNotFoundVersusOKSplit(t *testing.T) {
	publisher := &fakePublisher{}
	allowed := bearerDo(t, realtimeRouter(&fakePlans{}, publisher), http.MethodPost, realtimeAuthPath,
		`{"socketId":"123.456","channel":"private-plan-`+adminPlaceID+`"}`)
	if allowed.Code != http.StatusOK || publisher.calls != 1 {
		t.Fatalf("visible plan: status = %d, signed = %d, want 200 and one signature",
			allowed.Code, publisher.calls)
	}
	hidden := bearerDo(t, realtimeRouter(&fakePlans{watchErr: plans.ErrNotFound}, publisher),
		http.MethodPost, realtimeAuthPath, `{"socketId":"123.456","channel":"private-plan-`+adminPlaceID+`"}`)
	if hidden.Code != http.StatusNotFound || publisher.calls != 1 {
		t.Fatalf("invisible plan: status = %d, signed = %d, want 404 and no further signature",
			hidden.Code, publisher.calls)
	}
}
