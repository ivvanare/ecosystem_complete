package social

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Provider names as the contract spells them.
const (
	ProviderGoogle = "GOOGLE"
	ProviderApple  = "APPLE"
)

// OIDCVerifier validates ID tokens against each provider's discovery document.
//
// Providers are created lazily and cached: discovery costs a round trip, and a login is rare
// enough that paying it on first use is better than failing startup when a provider is down.
type OIDCVerifier struct {
	// clientIDs is keyed by provider name. A provider without a client id is not configured.
	clientIDs map[string]string
	// issuers overrides the discovery URL, for tests and for regional endpoints.
	issuers map[string]string

	mu    sync.Mutex
	cache map[string]*oidc.IDTokenVerifier
}

// NewOIDCVerifier builds a verifier from the configured client IDs.
func NewOIDCVerifier(googleClientID, appleClientID string) *OIDCVerifier {
	return &OIDCVerifier{
		clientIDs: map[string]string{
			ProviderGoogle: strings.TrimSpace(googleClientID),
			ProviderApple:  strings.TrimSpace(appleClientID),
		},
		issuers: map[string]string{
			ProviderGoogle: "https://accounts.google.com",
			ProviderApple:  "https://appleid.apple.com",
		},
		cache: map[string]*oidc.IDTokenVerifier{},
	}
}

// Configured reports whether the provider has a client id.
func (v *OIDCVerifier) Configured(provider string) bool {
	return v.clientIDs[strings.ToUpper(provider)] != ""
}

// Verify validates the token and returns the identity it proves.
//
// The audience is checked against the provider's client id, so a token minted for another
// application cannot be replayed here.
func (v *OIDCVerifier) Verify(ctx context.Context, provider, idToken string) (Identity, error) {
	provider = strings.ToUpper(strings.TrimSpace(provider))

	clientID := v.clientIDs[provider]
	if clientID == "" {
		return Identity{}, ErrUnknownProvider
	}

	verifier, err := v.verifierFor(ctx, provider)
	if err != nil {
		return Identity{}, fmt.Errorf("social: preparing verifier for %s: %w", provider, err)
	}

	raw, err := verifier.Verify(ctx, idToken)
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %s", ErrInvalidToken, err)
	}

	var claims struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified any    `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := raw.Claims(&claims); err != nil {
		return Identity{}, fmt.Errorf("%w: reading claims: %s", ErrInvalidToken, err)
	}
	if claims.Sub == "" {
		return Identity{}, fmt.Errorf("%w: token has no subject", ErrInvalidToken)
	}

	identity := Identity{
		Provider: provider,
		Subject:  claims.Sub,
		// Google sends a bool, Apple a string. Anything that is not clearly true stays false:
		// an unverified address must never be treated as proven.
		EmailVerified: claims.EmailVerified == true || claims.EmailVerified == "true",
	}
	if claims.Email != "" {
		email := strings.ToLower(strings.TrimSpace(claims.Email))
		identity.Email = &email
	}
	if claims.Name != "" {
		name := strings.TrimSpace(claims.Name)
		identity.Name = &name
	}

	return identity, nil
}

func (v *OIDCVerifier) verifierFor(ctx context.Context, provider string) (*oidc.IDTokenVerifier, error) {
	// Concurrent first logins would otherwise race on the map, and a map written from two
	// goroutines crashes the process.
	v.mu.Lock()
	defer v.mu.Unlock()

	if cached, ok := v.cache[provider]; ok {
		return cached, nil
	}

	issuer := v.issuers[provider]
	providerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	discovered, err := oidc.NewProvider(providerCtx, issuer)
	if err != nil {
		return nil, err
	}

	verifier := discovered.Verifier(&oidc.Config{ClientID: v.clientIDs[provider]})
	v.cache[provider] = verifier
	return verifier, nil
}
