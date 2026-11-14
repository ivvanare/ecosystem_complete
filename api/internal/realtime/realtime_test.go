package realtime

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The expected values were computed independently of this package, so the test pins the
// protocol instead of the implementation checking itself:
//
//	body_md5  = md5(`{"name":"vote.changed","channels":["private-plan-7"],"data":"{\"coRoute\":\"7\",\"coPlace\":\"9\"}"}`)
//	signature = HMAC-SHA256("app-secret", "POST\n/apps/app-id/events\n" + queryWithoutSignature)
//	auth      = "app-key:" + HMAC-SHA256("app-secret", "123.456:private-plan-7")
func fixedClient(t *testing.T) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: "http://soketi.invalid", AppID: "app-id", Key: "app-key", Secret: "app-secret"},
		slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	client.now = func() time.Time { return time.Unix(1700000000, 0) }
	return client
}

func sampleVote() VoteChanged { return VoteChanged{CoRoute: "7", CoPlace: "9"} }

func TestPublishSignsThePusherRequest(t *testing.T) {
	client := fixedClient(t)

	var gotPath, gotQuery, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotPath, gotQuery, gotBody = r.URL.Path, r.URL.RawQuery, string(raw)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client.baseURL = server.URL

	if err := client.Publish(context.Background(), Channel("7"), EventVoteChanged, sampleVote()); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if gotPath != "/apps/app-id/events" {
		t.Fatalf("path = %q", gotPath)
	}
	const wantQuery = "auth_key=app-key&auth_timestamp=1700000000&auth_version=1.0" +
		"&body_md5=f1b5b6482e42b92ae28f17cf8c6a4150" +
		"&auth_signature=00b8bcbd557af66ae0360785cb4adc90556e79819f09c6ef397f7ad89b63d8c9"
	if gotQuery != wantQuery {
		t.Fatalf("query = %q\nwant %q", gotQuery, wantQuery)
	}
	const wantBody = `{"name":"vote.changed","channels":["private-plan-7"],"data":"{\"coRoute\":\"7\",\"coPlace\":\"9\"}"}`
	if gotBody != wantBody {
		t.Fatalf("body = %s\nwant %s", gotBody, wantBody)
	}
}

// The subscription response is what the client hands to Soketi: it proves this API decided that
// socket may listen to that private channel.
func TestSubscriptionAuthSignsTheSocketAndChannel(t *testing.T) {
	client := fixedClient(t)
	auth, err := client.SubscriptionAuth("123.456", Channel("7"))
	if err != nil {
		t.Fatalf("SubscriptionAuth: %v", err)
	}
	const want = "app-key:2fb87413a2f3ac25d438cb7b0f5a1f25b35d168267da5ff71ba099d35ef612bb"
	if auth != want {
		t.Fatalf("auth = %q, want %q", auth, want)
	}
}

func TestSubscriptionAuthRefusesAChannelThatIsNotOurs(t *testing.T) {
	client := fixedClient(t)
	for _, channel := range []string{"plan-7", "private-plan-", "presence-plan-7"} {
		if _, err := client.SubscriptionAuth("123.456", channel); !errors.Is(err, ErrInvalidChannel) {
			t.Fatalf("channel %q: err = %v, want ErrInvalidChannel", channel, err)
		}
	}
}

func TestAPublishThatIsNot2xxIsAnError(t *testing.T) {
	client := fixedClient(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusInternalServerError)
	}))
	defer server.Close()
	client.baseURL = server.URL

	if err := client.Publish(context.Background(), Channel("7"), EventVoteChanged, sampleVote()); err == nil {
		t.Fatal("a 500 from Soketi must be an error, not silence")
	}
}

// Without credentials the service still runs: realtime is a courtesy on top of the vote.
func TestTheDisabledPublisherDoesNothing(t *testing.T) {
	var publisher Publisher = Disabled()
	if err := publisher.Publish(context.Background(), Channel("7"), EventVoteChanged, sampleVote()); err != nil {
		t.Fatalf("Publish on a disabled publisher = %v, want nil", err)
	}
	if _, err := publisher.SubscriptionAuth("123.456", Channel("7")); !errors.Is(err, ErrDisabled) {
		t.Fatalf("err = %v, want ErrDisabled", err)
	}
}

func TestNewRefusesAnIncompleteConfiguration(t *testing.T) {
	cases := []Config{
		{AppID: "app-id", Key: "app-key", Secret: "app-secret"},
		{BaseURL: "http://x", Key: "app-key", Secret: "app-secret"},
		{BaseURL: "http://x", AppID: "app-id", Secret: "app-secret"},
		{BaseURL: "http://x", AppID: "app-id", Key: "app-key"},
	}
	for _, cfg := range cases {
		if _, err := New(cfg, slog.New(slog.DiscardHandler)); err == nil {
			t.Fatalf("New(%+v) accepted an incomplete configuration", cfg)
		}
	}
}

func TestParseChannel(t *testing.T) {
	if planID, ok := ParseChannel("private-plan-7"); !ok || planID != "7" {
		t.Fatalf("ParseChannel = %q, %v", planID, ok)
	}
	for _, channel := range []string{"plan-7", "private-plan-", "private-plan", "presence-plan-7"} {
		if _, ok := ParseChannel(channel); ok {
			t.Fatalf("ParseChannel(%q) accepted an unrelated channel", channel)
		}
	}
}
