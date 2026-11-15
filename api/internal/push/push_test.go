package push

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

type fakeTokens struct {
	tokens []string
	err    error
	seen   string
}

func (f *fakeTokens) Tokens(_ context.Context, userID string) ([]string, error) {
	f.seen = userID
	return f.tokens, f.err
}

type fakePrefs struct {
	disabled []string
	err      error
}

func (f *fakePrefs) Disabled(context.Context, string) ([]string, error) {
	return f.disabled, f.err
}

type fakeSender struct {
	tokens  []string
	message Message
	err     error
	calls   int
}

func (f *fakeSender) Send(_ context.Context, tokens []string, message Message) error {
	f.calls++
	f.tokens, f.message = tokens, message
	return f.err
}

func newService(tokens Tokens, prefs Preferences, sender Sender) *Service {
	return New(tokens, prefs, sender, slog.New(slog.DiscardHandler))
}

func TestAPushGoesToEveryDeviceOfTheUser(t *testing.T) {
	sender := &fakeSender{}
	tokens := &fakeTokens{tokens: []string{"t1", "t2"}}
	message := Message{Title: "Te invitaron a un plan", Data: map[string]string{"kind": "invitation.received"}}

	newService(tokens, &fakePrefs{}, sender).Send(context.Background(), "u1", "invitation.received", message)

	if sender.calls != 1 {
		t.Fatalf("calls = %d, want one batch", sender.calls)
	}
	if len(sender.tokens) != 2 || sender.tokens[0] != "t1" {
		t.Fatalf("tokens = %v", sender.tokens)
	}
	if sender.message.Title != message.Title {
		t.Fatalf("message = %+v", sender.message)
	}
	if tokens.seen != "u1" {
		t.Fatalf("tokens read for %q", tokens.seen)
	}
}

// Without a provider there is nothing to do: the token is stored and unused, and that is a
// legitimate configuration, not an error to log on every event.
func TestNoSenderIsSilent(t *testing.T) {
	tokens := &fakeTokens{tokens: []string{"t1"}}
	newService(tokens, &fakePrefs{}, nil).Send(context.Background(), "u1", "k", Message{})
	if tokens.seen != "" {
		t.Fatal("the device list was read with no provider configured")
	}
}

// A disabled kind is the person's decision, and it is checked before anything is read.
func TestADisabledKindIsNotSent(t *testing.T) {
	sender := &fakeSender{}
	newService(&fakeTokens{tokens: []string{"t1"}},
		&fakePrefs{disabled: []string{"invitation.received", "review.received"}}, sender).
		Send(context.Background(), "u1", "invitation.received", Message{})

	if sender.calls != 0 {
		t.Fatal("a disabled kind was sent")
	}
}

// Someone who has not installed the app on a phone yet has no token: that is not a failure.
func TestNoDeviceIsNotAFailure(t *testing.T) {
	sender := &fakeSender{}
	newService(&fakeTokens{}, &fakePrefs{}, sender).Send(context.Background(), "u1", "k", Message{})
	if sender.calls != 0 {
		t.Fatal("sent with no device")
	}
}

// Every other failure is swallowed: the action that triggered the push already happened, and
// undoing it because a courtesy did not arrive would be worse than the courtesy.
func TestFailuresAreSwallowed(t *testing.T) {
	cases := []struct {
		name   string
		tokens Tokens
		prefs  Preferences
		sender Sender
	}{
		{"preferences down", &fakeTokens{tokens: []string{"t1"}}, &fakePrefs{err: errors.New("db")}, &fakeSender{}},
		{"devices down", &fakeTokens{err: errors.New("db")}, &fakePrefs{}, &fakeSender{}},
		{"provider down", &fakeTokens{tokens: []string{"t1"}}, &fakePrefs{}, &fakeSender{err: errors.New("503")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The call returning at all is the assertion: Send has no failure to report.
			newService(tc.tokens, tc.prefs, tc.sender).Send(context.Background(), "u1", "k", Message{})
		})
	}
}

// A provider failure is not retried here: retrying inside the request would hold the caller
// hostage to a third party, and the next event will try again anyway.
func TestAProviderFailureIsNotRetried(t *testing.T) {
	sender := &fakeSender{err: errors.New("503")}
	newService(&fakeTokens{tokens: []string{"t1"}}, &fakePrefs{}, sender).
		Send(context.Background(), "u1", "k", Message{})
	if sender.calls != 1 {
		t.Fatalf("calls = %d, want exactly one", sender.calls)
	}
}
