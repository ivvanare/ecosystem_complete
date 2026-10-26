package session

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeRotateTx struct {
	row       *TokenRow
	tokenErr  error
	revoked   int
	familyRev int
	inserted  []RefreshRecord
	commits   int
	rollbacks int
	insertErr error
}

func (f *fakeRotateTx) Token(context.Context) (TokenRow, error) {
	if f.tokenErr != nil {
		return TokenRow{}, f.tokenErr
	}
	if f.row == nil {
		return TokenRow{}, ErrNoToken
	}
	return *f.row, nil
}

func (f *fakeRotateTx) Revoke(context.Context) error { f.revoked++; return nil }

func (f *fakeRotateTx) RevokeFamily(context.Context) error { f.familyRev++; return nil }

func (f *fakeRotateTx) Insert(_ context.Context, record RefreshRecord) error {
	if f.insertErr != nil {
		return f.insertErr
	}
	f.inserted = append(f.inserted, record)
	return nil
}

func (f *fakeRotateTx) Commit(context.Context) error { f.commits++; return nil }

func (f *fakeRotateTx) Rollback(context.Context) error { f.rollbacks++; return nil }

type fakeRotateStore struct {
	tx  *fakeRotateTx
	err error
}

func (f *fakeRotateStore) BeginRotate(context.Context, string) (RotateTx, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.tx, nil
}

func newRotator(store RotateStore) *Rotator {
	rotator := NewRotator(store, Config{
		Secret:     []byte("un-secreto-de-pruebas-suficientemente-largo"),
		AccessTTL:  15 * time.Minute,
		RefreshTTL: 30 * 24 * time.Hour,
		Issuer:     "melia-api",
	})
	rotator.Now = func() time.Time { return testNow }
	return rotator
}

func liveRow() *TokenRow {
	return &TokenRow{
		UserID:    "user-1",
		Family:    "family-1",
		Device:    "iPhone",
		UserAgent: "k6",
		IP:        "10.0.0.1",
		ExpiresAt: testNow.Add(24 * time.Hour),
	}
}

func TestRefreshRotatesTheTokenInTheSameFamily(t *testing.T) {
	tx := &fakeRotateTx{row: liveRow()}
	rotator := newRotator(&fakeRotateStore{tx: tx})

	tokens, err := rotator.Refresh(context.Background(), "token-viejo", Meta{IP: "10.0.0.2"})
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}

	if tx.revoked != 1 {
		t.Errorf("revoked = %d, want 1: the presented token must die", tx.revoked)
	}
	if len(tx.inserted) != 1 {
		t.Fatalf("inserted %d tokens, want 1", len(tx.inserted))
	}
	record := tx.inserted[0]
	if record.Family != "family-1" {
		t.Errorf("family = %q, want the same family", record.Family)
	}
	if record.TokenHash == HashRefreshToken("token-viejo") {
		t.Error("the new token has the same hash as the old one")
	}
	if record.Device != "iPhone" || record.UserAgent != "k6" {
		t.Errorf("client metadata was not carried over: %+v", record)
	}
	if record.IP != "10.0.0.2" {
		t.Errorf("IP = %q, want the current request's", record.IP)
	}
	if tx.commits != 1 || tx.rollbacks != 0 {
		t.Errorf("commits=%d rollbacks=%d, want 1/0", tx.commits, tx.rollbacks)
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Error("no token pair returned")
	}
	if tokens.RefreshToken == "token-viejo" {
		t.Error("the old refresh token was returned again")
	}
}

func TestRefreshOnAReusedTokenRevokesTheWholeFamily(t *testing.T) {
	row := liveRow()
	revokedAt := testNow.Add(-time.Minute)
	row.RevokedAt = &revokedAt

	tx := &fakeRotateTx{row: row}
	rotator := newRotator(&fakeRotateStore{tx: tx})

	_, err := rotator.Refresh(context.Background(), "token-ya-usado", Meta{})
	if !errors.Is(err, ErrRefreshReused) {
		t.Fatalf("error = %v, want ErrRefreshReused", err)
	}
	if tx.familyRev != 1 {
		t.Errorf("familyRev = %d, want 1: reuse must kill the family", tx.familyRev)
	}
	if tx.commits != 1 {
		t.Error("the family revocation was not committed")
	}
	if len(tx.inserted) != 0 {
		t.Error("a new token was issued from a replayed one")
	}
	if tx.revoked != 0 {
		t.Error("Revoke was called on an already-revoked token")
	}
}

func TestRefreshRejectsAnExpiredToken(t *testing.T) {
	row := liveRow()
	row.ExpiresAt = testNow.Add(-time.Second)

	tx := &fakeRotateTx{row: row}
	rotator := newRotator(&fakeRotateStore{tx: tx})

	if _, err := rotator.Refresh(context.Background(), "token-vencido", Meta{}); !errors.Is(err, ErrInvalidRefresh) {
		t.Fatalf("error = %v, want ErrInvalidRefresh", err)
	}
	if len(tx.inserted) != 0 {
		t.Error("a new token was issued for an expired one")
	}
	if tx.rollbacks != 1 {
		t.Errorf("rollbacks = %d, want 1", tx.rollbacks)
	}
}

func TestRefreshRejectsAnUnknownToken(t *testing.T) {
	tx := &fakeRotateTx{}
	rotator := newRotator(&fakeRotateStore{tx: tx})

	if _, err := rotator.Refresh(context.Background(), "inventado", Meta{}); !errors.Is(err, ErrInvalidRefresh) {
		t.Fatalf("error = %v, want ErrInvalidRefresh", err)
	}
}

func TestRefreshRejectsAnEmptyToken(t *testing.T) {
	rotator := newRotator(&fakeRotateStore{tx: &fakeRotateTx{}})

	if _, err := rotator.Refresh(context.Background(), "", Meta{}); !errors.Is(err, ErrInvalidRefresh) {
		t.Fatalf("error = %v, want ErrInvalidRefresh", err)
	}
}

func TestLogoutRevokesTheFamily(t *testing.T) {
	tx := &fakeRotateTx{row: liveRow()}
	rotator := newRotator(&fakeRotateStore{tx: tx})

	if err := rotator.Logout(context.Background(), "token-activo"); err != nil {
		t.Fatalf("Logout returned error: %v", err)
	}
	if tx.familyRev != 1 {
		t.Errorf("familyRev = %d, want 1", tx.familyRev)
	}
	if tx.commits != 1 {
		t.Error("logout was not committed")
	}
}

func TestLogoutIsIdempotentForUnknownTokens(t *testing.T) {
	tx := &fakeRotateTx{}
	rotator := newRotator(&fakeRotateStore{tx: tx})

	if err := rotator.Logout(context.Background(), "inventado"); err != nil {
		t.Fatalf("Logout failed for an unknown token: %v", err)
	}
	if err := rotator.Logout(context.Background(), ""); err != nil {
		t.Fatalf("Logout failed for an empty token: %v", err)
	}
}
