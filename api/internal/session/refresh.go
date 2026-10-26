package session

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Refresh failures.
var (
	// ErrInvalidRefresh covers "unknown token" and "expired": neither tells the client
	// anything useful beyond "log in again".
	ErrInvalidRefresh = errors.New("session: refresh token is invalid or expired")
	// ErrRefreshReused means a token that had already been rotated was presented. The whole
	// family is revoked: either the client replayed it or someone stole it.
	ErrRefreshReused = errors.New("session: refresh token was already used")
	// ErrNoToken is returned by a RotateTx when the hash matches no row.
	ErrNoToken = errors.New("session: refresh token not found")
)

// TokenRow is the locked refresh row.
type TokenRow struct {
	UserID    string
	Family    string
	Device    string
	UserAgent string
	IP        string
	ExpiresAt time.Time
	RevokedAt *time.Time
}

// RotateTx is one transaction over a single refresh token, already holding its lock.
type RotateTx interface {
	// Token returns the locked row, or ErrNoToken.
	Token(ctx context.Context) (TokenRow, error)
	// Revoke marks this token as used.
	Revoke(ctx context.Context) error
	// RevokeFamily marks every token of the family as revoked.
	RevokeFamily(ctx context.Context) error
	// Insert adds the rotated token to the same family.
	Insert(ctx context.Context, record RefreshRecord) error
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// RotateStore opens the transaction for a token hash.
type RotateStore interface {
	BeginRotate(ctx context.Context, tokenHash string) (RotateTx, error)
}

// Rotator rotates refresh tokens and closes sessions.
type Rotator struct {
	Store  RotateStore
	Config Config
	Now    func() time.Time
}

// NewRotator returns a Rotator with the real clock.
func NewRotator(store RotateStore, cfg Config) *Rotator {
	return &Rotator{Store: store, Config: cfg, Now: time.Now}
}

// Refresh rotates the presented token and returns a new pair.
//
// Rotation is mandatory: the presented token is revoked in the same transaction that inserts
// its successor. Presenting an already-revoked token means it was replayed or stolen, so the
// entire family is revoked and the caller learns nothing beyond "log in again".
func (r *Rotator) Refresh(ctx context.Context, presented string, meta Meta) (Tokens, error) {
	if presented == "" {
		return Tokens{}, ErrInvalidRefresh
	}

	tx, err := r.Store.BeginRotate(ctx, HashRefreshToken(presented))
	if err != nil {
		if errors.Is(err, ErrNoToken) {
			return Tokens{}, ErrInvalidRefresh
		}
		return Tokens{}, fmt.Errorf("session: opening rotation: %w", err)
	}

	settled := false
	defer func() {
		if !settled {
			_ = tx.Rollback(ctx)
		}
	}()

	row, err := tx.Token(ctx)
	if err != nil {
		if errors.Is(err, ErrNoToken) {
			return Tokens{}, ErrInvalidRefresh
		}
		return Tokens{}, fmt.Errorf("session: reading refresh token: %w", err)
	}

	now := r.now()

	// Reuse first: a revoked token is the theft signal, and it must revoke the family even
	// if the token has also expired.
	if row.RevokedAt != nil {
		if err := tx.RevokeFamily(ctx); err != nil {
			return Tokens{}, fmt.Errorf("session: revoking family: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return Tokens{}, fmt.Errorf("session: committing family revocation: %w", err)
		}
		settled = true
		return Tokens{}, ErrRefreshReused
	}

	if !row.ExpiresAt.After(now) {
		return Tokens{}, ErrInvalidRefresh
	}

	if err := tx.Revoke(ctx); err != nil {
		return Tokens{}, fmt.Errorf("session: revoking presented token: %w", err)
	}

	newToken, newHash, err := newRefreshToken()
	if err != nil {
		return Tokens{}, err
	}
	expiresAt := now.Add(r.Config.RefreshTTL)

	// Client metadata is carried over: the device does not change because the token rotated.
	if err := tx.Insert(ctx, RefreshRecord{
		UserID:    row.UserID,
		TokenHash: newHash,
		Family:    row.Family,
		Device:    row.Device,
		UserAgent: row.UserAgent,
		IP:        meta.IP,
		ExpiresAt: expiresAt,
	}); err != nil {
		return Tokens{}, fmt.Errorf("session: storing rotated token: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Tokens{}, fmt.Errorf("session: committing rotation: %w", err)
	}
	settled = true

	accessToken, err := signAccess(r.Config, row.UserID, now)
	if err != nil {
		return Tokens{}, err
	}

	return Tokens{
		AccessToken:      accessToken,
		ExpiresIn:        int(r.Config.AccessTTL.Seconds()),
		RefreshToken:     newToken,
		RefreshExpiresIn: int(r.Config.RefreshTTL.Seconds()),
	}, nil
}

// Logout revokes the family of the presented token.
//
// Idempotent by design: an unknown or already-revoked token still answers success, because
// "you are logged out" is the desired state either way and an error would leak whether the
// token existed.
func (r *Rotator) Logout(ctx context.Context, presented string) error {
	if presented == "" {
		return nil
	}

	tx, err := r.Store.BeginRotate(ctx, HashRefreshToken(presented))
	if err != nil {
		if errors.Is(err, ErrNoToken) {
			return nil
		}
		return fmt.Errorf("session: opening logout: %w", err)
	}

	settled := false
	defer func() {
		if !settled {
			_ = tx.Rollback(ctx)
		}
	}()

	if _, err := tx.Token(ctx); err != nil {
		if errors.Is(err, ErrNoToken) {
			return nil
		}
		return fmt.Errorf("session: reading refresh token: %w", err)
	}

	if err := tx.RevokeFamily(ctx); err != nil {
		return fmt.Errorf("session: revoking family: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("session: committing logout: %w", err)
	}
	settled = true

	return nil
}

func (r *Rotator) now() time.Time {
	if r.Now == nil {
		return time.Now()
	}
	return r.Now()
}
