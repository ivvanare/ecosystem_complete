// Package session issues and validates the two credentials the contract defines: a short
// stateless access JWT and a long rotating refresh token.
//
// The access token carries identity only — `sub`, `iat`, `exp`, `jti` — and never roles or
// permissions. A token that embedded capability would keep a revoked role alive until it
// expired, and the model requires capability to come from live RBAC.
package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Errors the callers translate into contract codes.
var (
	ErrInvalidToken = errors.New("session: token is not valid")
	ErrMissingSub   = errors.New("session: token has no subject")
)

// Config is the signing configuration.
type Config struct {
	Secret     []byte
	AccessTTL  time.Duration
	RefreshTTL time.Duration
	Issuer     string
}

// Tokens is the pair returned to a client.
type Tokens struct {
	AccessToken      string
	ExpiresIn        int
	RefreshToken     string
	RefreshExpiresIn int
}

// RefreshRecord is one row of seg.tb_refresh_token. Only the hash of the token is stored.
type RefreshRecord struct {
	UserID    string
	TokenHash string
	Family    string
	Device    string
	UserAgent string
	IP        string
	ExpiresAt time.Time
}

// RefreshStore persists the refresh token.
type RefreshStore interface {
	StoreRefreshToken(ctx context.Context, record RefreshRecord) error
}

// Meta is the client context recorded with the session.
type Meta struct {
	Device    string
	UserAgent string
	IP        string
}

// Claims is what the access token carries.
type Claims struct {
	jwt.RegisteredClaims
}

// Issuer signs access tokens and registers refresh tokens.
type Issuer struct {
	cfg   Config
	store RefreshStore
	now   func() time.Time
}

// New returns an Issuer. The store is only touched when issuing.
func New(cfg Config, store RefreshStore) *Issuer {
	return &Issuer{cfg: cfg, store: store, now: time.Now}
}

// SetClock overrides the clock, for tests.
func (i *Issuer) SetClock(now func() time.Time) { i.now = now }

// Issue creates a new family with its first refresh token and signs an access token.
func (i *Issuer) Issue(ctx context.Context, userID string, meta Meta) (Tokens, error) {
	if userID == "" {
		return Tokens{}, ErrMissingSub
	}

	refreshToken, refreshHash, err := newRefreshToken()
	if err != nil {
		return Tokens{}, err
	}

	family, err := newID()
	if err != nil {
		return Tokens{}, err
	}

	now := i.now()
	refreshExpiry := now.Add(i.cfg.RefreshTTL)

	if err := i.store.StoreRefreshToken(ctx, RefreshRecord{
		UserID:    userID,
		TokenHash: refreshHash,
		Family:    family,
		Device:    meta.Device,
		UserAgent: meta.UserAgent,
		IP:        meta.IP,
		ExpiresAt: refreshExpiry,
	}); err != nil {
		return Tokens{}, fmt.Errorf("session: storing refresh token: %w", err)
	}

	accessToken, err := i.sign(userID, now)
	if err != nil {
		return Tokens{}, err
	}

	return Tokens{
		AccessToken:      accessToken,
		ExpiresIn:        int(i.cfg.AccessTTL.Seconds()),
		RefreshToken:     refreshToken,
		RefreshExpiresIn: int(i.cfg.RefreshTTL.Seconds()),
	}, nil
}

func (i *Issuer) sign(userID string, now time.Time) (string, error) {
	return signAccess(i.cfg, userID, now)
}

// signAccess is shared by issuance and rotation: both produce the same kind of token.
func signAccess(cfg Config, userID string, now time.Time) (string, error) {
	jti, err := newID()
	if err != nil {
		return "", err
	}

	claims := Claims{RegisteredClaims: jwt.RegisteredClaims{
		Subject:   userID,
		Issuer:    cfg.Issuer,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(cfg.AccessTTL)),
		ID:        jti,
	}}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(cfg.Secret)
	if err != nil {
		return "", fmt.Errorf("session: signing access token: %w", err)
	}
	return signed, nil
}

// Parse validates the signature, the algorithm and the expiry, and returns the claims.
//
// The algorithm is checked explicitly: accepting whatever the token declares is the classic
// algorithm-confusion hole, where an attacker re-signs with a weaker method.
func (i *Issuer) Parse(raw string) (Claims, error) {
	var claims Claims

	parsed, err := jwt.ParseWithClaims(raw, &claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("%w: unexpected signing method %s", ErrInvalidToken, token.Method.Alg())
		}
		return i.cfg.Secret, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		// Validate against the issuer's clock, not the wall clock: one source of time keeps
		// issue and parse consistent and makes expiry testable.
		jwt.WithTimeFunc(i.now),
	)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: %s", ErrInvalidToken, err)
	}
	if !parsed.Valid {
		return Claims{}, ErrInvalidToken
	}
	if claims.Subject == "" {
		return Claims{}, ErrMissingSub
	}

	return claims, nil
}

// newRefreshToken returns the opaque token and its SHA-256 hash. The clear value exists
// only in the response; the database keeps the hash, so a dump cannot be replayed.
func newRefreshToken() (token, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("session: reading refresh entropy: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, HashRefreshToken(token), nil
}

// HashRefreshToken is exported so refresh rotation can look a presented token up by hash.
func HashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func newID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("session: reading id entropy: %w", err)
	}
	return hex.EncodeToString(raw), nil
}
