package main

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/ivvanare/ecosystem_complete/api/internal/config"
)

// Realtime is optional as a whole, and "optional" has to mean a nil publisher: the router
// registers the subscription route on a non-nil publisher, so wiring a no-op implementation here
// made an optional route permanent and it answered 500 instead of 404.
func TestAnUnconfiguredBrokerBuildsNoClient(t *testing.T) {
	client, err := newRealtimeClient(config.Config{}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("an unconfigured broker is not an error: %v", err)
	}
	if client != nil {
		t.Fatalf("client = %+v, want nil so the route stays absent", client)
	}
}

func TestAConfiguredBrokerBuildsAClient(t *testing.T) {
	cfg := config.Config{
		SoketiURL:    "http://soketi:6001",
		SoketiAppID:  "app-id",
		SoketiKey:    "app-key",
		SoketiSecret: "app-secret",
	}
	client, err := newRealtimeClient(cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("newRealtimeClient: %v", err)
	}
	if client == nil {
		t.Fatal("a configured broker built no client")
	}
}

// Half a configuration must not reach the constructor: config.Load refuses it first, but the
// wiring must not paper over it if that ever changes.
func TestAHalfConfiguredBrokerIsAnError(t *testing.T) {
	cfg := config.Config{SoketiURL: "http://soketi:6001", SoketiAppID: "app-id"}
	_, err := newRealtimeClient(cfg, slog.New(slog.DiscardHandler))
	if err == nil {
		t.Fatal("a half-configured broker was accepted")
	}
	if !strings.Contains(err.Error(), "SOKETI_KEY") {
		t.Fatalf("the error does not name the missing credential: %v", err)
	}
}
