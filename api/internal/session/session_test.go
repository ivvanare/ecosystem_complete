package session

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type fakeStore struct {
	records []RefreshRecord
	err     error
}

func (f *fakeStore) StoreRefreshToken(_ context.Context, record RefreshRecord) error {
	f.records = append(f.records, record)
	return f.err
}

var testNow = time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)

func newIssuer(t *testing.T, store RefreshStore) *Issuer {
	t.Helper()
	issuer := New(Config{
		Secret:     []byte("un-secreto-de-pruebas-suficientemente-largo"),
		AccessTTL:  15 * time.Minute,
		RefreshTTL: 30 * 24 * time.Hour,
		Issuer:     "melia-api",
	}, store)
	issuer.SetClock(func() time.Time { return testNow })
	return issuer
}

func TestIssueReturnsAParsableAccessToken(t *testing.T) {
	issuer := newIssuer(t, &fakeStore{})

	tokens, err := issuer.Issue(context.Background(), "user-1", Meta{Device: "iPhone de Ana"})
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}

	if tokens.ExpiresIn != 900 {
		t.Errorf("ExpiresIn = %d, want 900", tokens.ExpiresIn)
	}
	if tokens.RefreshExpiresIn != 30*24*3600 {
		t.Errorf("RefreshExpiresIn = %d", tokens.RefreshExpiresIn)
	}

	claims, err := issuer.Parse(tokens.AccessToken)
	if err != nil {
		t.Fatalf("Parse rejected a freshly issued token: %v", err)
	}
	if claims.Subject != "user-1" {
		t.Errorf("subject = %q, want user-1", claims.Subject)
	}
	if claims.ID == "" {
		t.Error("token has no jti")
	}
	if !claims.ExpiresAt.Time.Equal(testNow.Add(15 * time.Minute)) {
		t.Errorf("exp = %v, want now+15m", claims.ExpiresAt.Time)
	}
}

func TestAccessTokenCarriesNoCapability(t *testing.T) {
	issuer := newIssuer(t, &fakeStore{})
	tokens, err := issuer.Issue(context.Background(), "user-1", Meta{})
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}

	claims, err := issuer.Parse(tokens.AccessToken)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	// Only the registered claims exist. A role or permission smuggled into the token would
	// outlive its revocation, so nothing beyond identity belongs here.
	if claims.Subject == "" || claims.Issuer != "melia-api" {
		t.Errorf("unexpected claims: %+v", claims)
	}
}

func TestRefreshTokenIsStoredHashedOnly(t *testing.T) {
	store := &fakeStore{}
	issuer := newIssuer(t, store)

	tokens, err := issuer.Issue(context.Background(), "user-1", Meta{Device: "iPhone", IP: "10.0.0.1"})
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}

	if len(store.records) != 1 {
		t.Fatalf("stored %d refresh records, want 1", len(store.records))
	}
	record := store.records[0]

	if strings.Contains(record.TokenHash, tokens.RefreshToken) {
		t.Fatal("the stored hash contains the clear refresh token")
	}
	if record.TokenHash != HashRefreshToken(tokens.RefreshToken) {
		t.Error("the stored hash is not the SHA-256 of the issued token")
	}
	if record.Family == "" {
		t.Error("the record has no family: reuse detection needs it")
	}
	if !record.ExpiresAt.Equal(testNow.Add(30 * 24 * time.Hour)) {
		t.Errorf("ExpiresAt = %v", record.ExpiresAt)
	}
	if record.Device != "iPhone" || record.IP != "10.0.0.1" {
		t.Errorf("client metadata lost: %+v", record)
	}
}

func TestIssueUsesANewFamilyPerSession(t *testing.T) {
	store := &fakeStore{}
	issuer := newIssuer(t, store)

	if _, err := issuer.Issue(context.Background(), "user-1", Meta{}); err != nil {
		t.Fatalf("first Issue: %v", err)
	}
	if _, err := issuer.Issue(context.Background(), "user-1", Meta{}); err != nil {
		t.Fatalf("second Issue: %v", err)
	}

	if store.records[0].Family == store.records[1].Family {
		t.Error("two logins share a family: revoking one session would kill the other")
	}
}

func TestParseRejectsAnotherSecret(t *testing.T) {
	issuer := newIssuer(t, &fakeStore{})
	tokens, err := issuer.Issue(context.Background(), "user-1", Meta{})
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}

	other := New(Config{
		Secret:    []byte("otro-secreto-distinto-y-suficientemente-largo"),
		AccessTTL: 15 * time.Minute,
	}, &fakeStore{})

	if _, err := other.Parse(tokens.AccessToken); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("Parse error = %v, want ErrInvalidToken", err)
	}
}

func TestParseRejectsExpiredToken(t *testing.T) {
	issuer := newIssuer(t, &fakeStore{})
	tokens, err := issuer.Issue(context.Background(), "user-1", Meta{})
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}

	issuer.SetClock(func() time.Time { return testNow.Add(16 * time.Minute) })
	if _, err := issuer.Parse(tokens.AccessToken); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("Parse accepted an expired token: %v", err)
	}
}

func TestParseRejectsAnotherAlgorithm(t *testing.T) {
	issuer := newIssuer(t, &fakeStore{})

	// Same secret, different algorithm: the classic confusion attempt.
	claims := Claims{RegisteredClaims: jwt.RegisteredClaims{
		Subject:   "user-1",
		ExpiresAt: jwt.NewNumericDate(testNow.Add(time.Hour)),
	}}
	forged, err := jwt.NewWithClaims(jwt.SigningMethodHS512, claims).SignedString([]byte("un-secreto-de-pruebas-suficientemente-largo"))
	if err != nil {
		t.Fatalf("forging token: %v", err)
	}

	if _, err := issuer.Parse(forged); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("Parse accepted an HS512 token: %v", err)
	}
}

func TestIssueFailsWhenTheStoreFails(t *testing.T) {
	issuer := newIssuer(t, &fakeStore{err: errors.New("db down")})

	if _, err := issuer.Issue(context.Background(), "user-1", Meta{}); err == nil {
		t.Fatal("Issue succeeded, want error")
	}
}

func TestIssueRequiresASubject(t *testing.T) {
	issuer := newIssuer(t, &fakeStore{})

	if _, err := issuer.Issue(context.Background(), "", Meta{}); !errors.Is(err, ErrMissingSub) {
		t.Errorf("error = %v, want ErrMissingSub", err)
	}
}
