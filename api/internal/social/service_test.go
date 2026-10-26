package social

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/ivvanare/ecosystem_complete/api/internal/audit"
	"github.com/ivvanare/ecosystem_complete/api/internal/session"
)

type fakeVerifier struct {
	identity Identity
	err      error
	provider string
}

func (f *fakeVerifier) Verify(_ context.Context, provider, _ string) (Identity, error) {
	f.provider = provider
	return f.identity, f.err
}

type fakeStore struct {
	userID    string
	lookupErr error
	created   []Identity
	createID  string
}

func (f *fakeStore) UserIDByIdentity(context.Context, string, string) (string, error) {
	if f.lookupErr != nil {
		return "", f.lookupErr
	}
	return f.userID, nil
}

func (f *fakeStore) CreateUserWithIdentity(_ context.Context, identity Identity) (string, error) {
	f.created = append(f.created, identity)
	return f.createID, nil
}

type fakeIssuer struct {
	calls int
	user  string
	err   error
}

func (f *fakeIssuer) Issue(_ context.Context, userID string, _ session.Meta) (session.Tokens, error) {
	f.calls++
	f.user = userID
	if f.err != nil {
		return session.Tokens{}, f.err
	}
	return session.Tokens{AccessToken: "a", RefreshToken: "r"}, nil
}

type fakeAuditor struct{ events []audit.Event }

func (f *fakeAuditor) Record(_ context.Context, event audit.Event) error {
	f.events = append(f.events, event)
	return nil
}

func TestAuthenticateReusesAKnownIdentity(t *testing.T) {
	store := &fakeStore{userID: "user-1"}
	issuer := &fakeIssuer{}
	auditor := &fakeAuditor{}
	service := New(&fakeVerifier{identity: Identity{Provider: ProviderGoogle, Subject: "sub-1"}},
		store, issuer, auditor, slog.New(slog.DiscardHandler))

	if _, err := service.Authenticate(context.Background(), ProviderGoogle, "token", session.Meta{}); err != nil {
		t.Fatalf("Authenticate returned error: %v", err)
	}
	if len(store.created) != 0 {
		t.Error("a known identity created another account")
	}
	if issuer.user != "user-1" {
		t.Errorf("issued for %q", issuer.user)
	}
	if len(auditor.events) != 1 || auditor.events[0].Name != audit.EventSocialLogin {
		t.Errorf("audit = %v", auditor.events)
	}
}

func TestAuthenticateCreatesTheAccountOnFirstSight(t *testing.T) {
	email := "ana@melia.test"
	store := &fakeStore{lookupErr: ErrNoIdentity, createID: "user-9"}
	auditor := &fakeAuditor{}
	service := New(&fakeVerifier{identity: Identity{
		Provider: ProviderApple, Subject: "sub-9", Email: &email, EmailVerified: true,
	}}, store, &fakeIssuer{}, auditor, slog.New(slog.DiscardHandler))

	if _, err := service.Authenticate(context.Background(), ProviderApple, "token", session.Meta{}); err != nil {
		t.Fatalf("Authenticate returned error: %v", err)
	}
	if len(store.created) != 1 || store.created[0].Subject != "sub-9" {
		t.Fatalf("created = %v", store.created)
	}
	names := make([]string, 0, len(auditor.events))
	for _, event := range auditor.events {
		names = append(names, event.Name)
	}
	if len(names) != 2 || names[0] != audit.EventSocialAccountCreated || names[1] != audit.EventSocialLogin {
		t.Errorf("audit = %v, want account_created then login", names)
	}
}

func TestAuthenticateRejectsAnInvalidToken(t *testing.T) {
	auditor := &fakeAuditor{}
	service := New(&fakeVerifier{err: ErrInvalidToken}, &fakeStore{}, &fakeIssuer{}, auditor,
		slog.New(slog.DiscardHandler))

	if _, err := service.Authenticate(context.Background(), ProviderGoogle, "basura", session.Meta{}); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("error = %v, want ErrInvalidToken", err)
	}
	if len(auditor.events) != 1 || auditor.events[0].Name != audit.EventSocialLoginFailed {
		t.Errorf("audit = %v", auditor.events)
	}
}

func TestAuthenticateRejectsAnUnconfiguredProvider(t *testing.T) {
	service := New(&fakeVerifier{err: ErrUnknownProvider}, &fakeStore{}, &fakeIssuer{}, &fakeAuditor{},
		slog.New(slog.DiscardHandler))

	if _, err := service.Authenticate(context.Background(), ProviderApple, "token", session.Meta{}); !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("error = %v, want ErrUnknownProvider", err)
	}
}

func TestAuthenticatePropagatesIssuerFailures(t *testing.T) {
	store := &fakeStore{userID: "user-1"}
	issuer := &fakeIssuer{err: errors.New("db down")}
	service := New(&fakeVerifier{identity: Identity{Provider: ProviderGoogle, Subject: "s"}},
		store, issuer, &fakeAuditor{}, slog.New(slog.DiscardHandler))

	if _, err := service.Authenticate(context.Background(), ProviderGoogle, "token", session.Meta{}); err == nil {
		t.Fatal("Authenticate succeeded despite the issuer failing")
	}
}

func TestOIDCVerifierRejectsAnUnconfiguredProvider(t *testing.T) {
	verifier := NewOIDCVerifier("", "")

	if verifier.Configured(ProviderGoogle) {
		t.Error("an empty client id counts as configured")
	}
	if _, err := verifier.Verify(context.Background(), ProviderGoogle, "token"); !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("error = %v, want ErrUnknownProvider", err)
	}
}
