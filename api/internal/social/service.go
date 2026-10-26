// Package social authenticates a user with a provider-issued ID token (Google or Apple).
//
// The identity is resolved by (provider, subject), never by email: Apple invents relay
// addresses and may withhold the address entirely. The provider token is never a credential
// for this API — it is validated and exchanged for our own JWT.
package social

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ivvanare/ecosystem_complete/api/internal/audit"
	"github.com/ivvanare/ecosystem_complete/api/internal/session"
)

// Errors mapped onto the contract.
var (
	// ErrInvalidToken means the provider token did not validate.
	ErrInvalidToken = errors.New("social: the provider token is not valid")
	// ErrUnknownProvider means the provider is not configured on this server.
	ErrUnknownProvider = errors.New("social: provider is not configured")
	// ErrNoIdentity is returned by a Store when nobody is linked to that subject yet.
	ErrNoIdentity = errors.New("social: no identity linked")
)

// Identity is what a validated provider token proves.
type Identity struct {
	Provider      string
	Subject       string
	Email         *string
	EmailVerified bool
	Name          *string
}

// Verifier validates an ID token against the provider's JWKS.
type Verifier interface {
	Verify(ctx context.Context, provider, idToken string) (Identity, error)
}

// Store links provider subjects to users.
type Store interface {
	// UserIDByIdentity returns the user linked to (provider, subject), or ErrNoIdentity.
	UserIDByIdentity(ctx context.Context, provider, subject string) (string, error)
	// CreateUserWithIdentity creates the user and the link, returning the user id.
	CreateUserWithIdentity(ctx context.Context, identity Identity) (string, error)
}

// Issuer opens the session.
type Issuer interface {
	Issue(ctx context.Context, userID string, meta session.Meta) (session.Tokens, error)
}

// Service authenticates federated logins.
type Service struct {
	Verifier Verifier
	Store    Store
	Issuer   Issuer
	Auditor  audit.Recorder
	Logger   *slog.Logger
}

// New returns a Service.
func New(verifier Verifier, store Store, issuer Issuer, auditor audit.Recorder, logger *slog.Logger) *Service {
	return &Service{Verifier: verifier, Store: store, Issuer: issuer, Auditor: auditor, Logger: logger}
}

// Authenticate validates the token, resolves the account and opens a session.
//
// First sight of a (provider, subject) creates the account. An email that already belongs to
// another account is never used to link them: the model resolves identity by subject, and
// linking by email would let a provider that returns an unverified address take over an
// existing account.
func (s *Service) Authenticate(ctx context.Context, provider, idToken string, meta session.Meta) (session.Tokens, error) {
	identity, err := s.Verifier.Verify(ctx, provider, idToken)
	if err != nil {
		if errors.Is(err, ErrUnknownProvider) {
			return session.Tokens{}, ErrUnknownProvider
		}
		s.record(ctx, audit.Event{
			Name:      audit.EventSocialLoginFailed,
			Metadata:  map[string]any{"provider": provider, "reason": "invalid_token"},
			IP:        meta.IP,
			UserAgent: meta.UserAgent,
		})
		return session.Tokens{}, ErrInvalidToken
	}

	userID, err := s.Store.UserIDByIdentity(ctx, identity.Provider, identity.Subject)
	switch {
	case err == nil:
		// Known identity: nothing to create.
	case errors.Is(err, ErrNoIdentity):
		userID, err = s.Store.CreateUserWithIdentity(ctx, identity)
		if err != nil {
			return session.Tokens{}, fmt.Errorf("social: creating user: %w", err)
		}
		s.record(ctx, audit.Event{
			Name:      audit.EventSocialAccountCreated,
			UserID:    userID,
			Metadata:  map[string]any{"provider": identity.Provider},
			IP:        meta.IP,
			UserAgent: meta.UserAgent,
		})
	default:
		return session.Tokens{}, fmt.Errorf("social: resolving identity: %w", err)
	}

	tokens, err := s.Issuer.Issue(ctx, userID, meta)
	if err != nil {
		return session.Tokens{}, err
	}

	s.record(ctx, audit.Event{
		Name:      audit.EventSocialLogin,
		UserID:    userID,
		Metadata:  map[string]any{"provider": identity.Provider},
		IP:        meta.IP,
		UserAgent: meta.UserAgent,
	})

	return tokens, nil
}

// record is best effort: the audit log never decides whether a login succeeded.
func (s *Service) record(ctx context.Context, event audit.Event) {
	if s.Auditor == nil {
		return
	}
	if err := s.Auditor.Record(ctx, event); err != nil && s.Logger != nil {
		s.Logger.Warn("audit write failed", slog.String("event", event.Name), slog.String("error", err.Error()))
	}
}
