// Package realtime publishes events to Soketi (the Pusher protocol) and signs the subscription
// requests for its private channels.
//
// The rule this package exists to respect: an event is a courtesy, the write is the fact. A
// publish failure is logged by the caller and never turns a vote into an error (S4).
package realtime

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// channelPrefix is the Pusher convention for a channel whose subscription must be
	// authorized by the server: the prefix is what forces the client to ask.
	channelPrefix = "private-plan-"

	// EventVoteChanged is the only event this slice publishes. The payload is a hint, never the
	// state: a client that misses it re-reads the plan (S1).
	EventVoteChanged = "vote.changed"

	signatureVersion = "1.0"
)

var (
	// ErrDisabled is what a disabled publisher answers when someone asks it to sign: the route
	// is not registered in that case, so reaching it is a wiring mistake worth naming.
	ErrDisabled = errors.New("realtime: publisher is disabled")
	// ErrInvalidChannel is a channel this service does not own: refusing to sign for it keeps
	// the credential from authorizing someone else's channel.
	ErrInvalidChannel = errors.New("realtime: channel is not a plan channel")
)

// Config is the Pusher application credentials Soketi is configured with.
type Config struct {
	BaseURL string
	AppID   string
	Key     string
	Secret  string
}

// VoteChanged is the published payload: which plan, which place. No votes, no ranking — those
// are read from the plan itself.
type VoteChanged struct {
	CoRoute string `json:"coRoute"`
	CoPlace string `json:"coPlace"`
}

// Publisher publishes one event to one channel and signs subscription requests.
type Publisher interface {
	Publish(ctx context.Context, channel, event string, data any) error
	SubscriptionAuth(socketID, channel string) (string, error)
}

// Channel is the private channel of a plan.
func Channel(planID string) string { return channelPrefix + planID }

// ParseChannel returns the plan id of one of our private channels, and whether it is ours at all.
func ParseChannel(channel string) (string, bool) {
	if !strings.HasPrefix(channel, channelPrefix) {
		return "", false
	}
	planID := strings.TrimPrefix(channel, channelPrefix)
	if planID == "" {
		return "", false
	}
	return planID, true
}

// Client is the Pusher HTTP implementation.
type Client struct {
	baseURL string
	appID   string
	key     string
	secret  string
	http    *http.Client
	logger  *slog.Logger
	// now is a field, not a call, so the signature is provable against a fixed vector.
	now func() time.Time
}

// New builds a client. Every credential is required: half a configuration is not a degraded
// service, it is a publisher that would fail on its first event.
func New(cfg Config, logger *slog.Logger) (*Client, error) {
	missing := make([]string, 0, 4)
	switch {
	case strings.TrimSpace(cfg.BaseURL) == "":
		missing = append(missing, "SOKETI_URL")
	}
	if strings.TrimSpace(cfg.AppID) == "" {
		missing = append(missing, "SOKETI_APP_ID")
	}
	if strings.TrimSpace(cfg.Key) == "" {
		missing = append(missing, "SOKETI_KEY")
	}
	if strings.TrimSpace(cfg.Secret) == "" {
		missing = append(missing, "SOKETI_SECRET")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("realtime: missing %s", strings.Join(missing, ", "))
	}

	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		appID:   cfg.AppID,
		key:     cfg.Key,
		secret:  cfg.Secret,
		http:    &http.Client{Timeout: 5 * time.Second},
		logger:  logger,
		now:     time.Now,
	}, nil
}

// event is the Pusher event envelope. A struct, not a map: the field order is part of the body
// the signature covers, and a map's alphabetical order is an accident of encoding/json.
type event struct {
	Name     string   `json:"name"`
	Channels []string `json:"channels"`
	Data     string   `json:"data"`
}

// Publish sends one event to one channel. The signature covers the query without itself, which
// is why it is appended after signing.
func (c *Client) Publish(ctx context.Context, channel, eventName string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("realtime: encoding the payload: %w", err)
	}
	body, err := json.Marshal(event{
		Name:     eventName,
		Channels: []string{channel},
		Data:     string(payload),
	})
	if err != nil {
		return fmt.Errorf("realtime: encoding the event: %w", err)
	}

	sum := md5.Sum(body)
	query := url.Values{}
	query.Set("auth_key", c.key)
	query.Set("auth_timestamp", fmt.Sprintf("%d", c.now().Unix()))
	query.Set("auth_version", signatureVersion)
	query.Set("body_md5", hex.EncodeToString(sum[:]))
	signature := c.sign("POST", c.eventsPath(), query.Encode())

	// auth_signature is part of the URL, as Pusher documents; it is appended after the string
	// was built so the signature does not cover itself.
	request := c.baseURL + c.eventsPath() + "?" + query.Encode() + "&auth_signature=" + signature

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, request, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("realtime: building the request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")

	response, err := c.http.Do(httpRequest)
	if err != nil {
		return fmt.Errorf("realtime: publishing %s: %w", eventName, err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode < 200 || response.StatusCode > 299 {
		snippet, _ := io.ReadAll(io.LimitReader(response.Body, 256))
		return fmt.Errorf("realtime: publishing %s: soketi answered %d: %s",
			eventName, response.StatusCode, strings.TrimSpace(string(snippet)))
	}
	return nil
}

// SubscriptionAuth signs the subscription of one socket to one of our private channels. The
// caller has already decided that this socket may watch: the signature is the proof it hands
// to Soketi.
func (c *Client) SubscriptionAuth(socketID, channel string) (string, error) {
	if _, ok := ParseChannel(channel); !ok {
		return "", ErrInvalidChannel
	}
	if strings.TrimSpace(socketID) == "" {
		return "", fmt.Errorf("realtime: a subscription needs a socket id")
	}
	mac := hmac.New(sha256.New, []byte(c.secret))
	mac.Write([]byte(socketID + ":" + channel))
	return c.key + ":" + hex.EncodeToString(mac.Sum(nil)), nil
}

// eventsPath is signed as-is, so it is built in one place: a path that differs by a slash
// between the signature and the request is a 401 that reads like a credential problem.
func (c *Client) eventsPath() string { return "/apps/" + c.appID + "/events" }

func (c *Client) sign(method, path, query string) string {
	mac := hmac.New(sha256.New, []byte(c.secret))
	mac.Write([]byte(method + "\n" + path + "\n" + query))
	return hex.EncodeToString(mac.Sum(nil))
}

// disabled is the publisher used when no credentials are configured.
type disabled struct{}

// Disabled returns a publisher that does nothing and signs nothing.
func Disabled() Publisher { return disabled{} }

func (disabled) Publish(context.Context, string, string, any) error { return nil }

func (disabled) SubscriptionAuth(string, string) (string, error) { return "", ErrDisabled }
