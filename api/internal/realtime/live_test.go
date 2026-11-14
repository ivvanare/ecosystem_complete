package realtime

import (
	"context"
	"log/slog"
	"os"
	"testing"
)

// This test needs a running Soketi. It is skipped by default because the suite must pass without
// services; run it with the broker up to prove the protocol, not just the arithmetic:
//
//	SOKETI_LIVE_URL=http://localhost:6001 go test ./internal/realtime/ -run Live -v
func TestLivePublishAgainstTheRunningBroker(t *testing.T) {
	baseURL := os.Getenv("SOKETI_LIVE_URL")
	if baseURL == "" {
		t.Skip("set SOKETI_LIVE_URL to publish against a running Soketi")
	}
	client, err := New(Config{
		BaseURL: baseURL,
		AppID:   envOr("SOKETI_LIVE_APP_ID", "app-id"),
		Key:     envOr("SOKETI_LIVE_KEY", "app-key"),
		Secret:  envOr("SOKETI_LIVE_SECRET", "app-secret"),
	}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// A channel nobody is subscribed to: the broker still answers, which is what proves the
	// signature and the body are acceptable to it.
	err = client.Publish(context.Background(), Channel("live-test"),
		EventVoteChanged, VoteChanged{CoRoute: "live-test", CoPlace: "live-test"})
	if err != nil {
		t.Fatalf("the broker rejected a signed publish: %v", err)
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
