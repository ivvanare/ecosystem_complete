// Package avatar owns the one picture a person publishes about themselves.
//
// It stores a URL, never bytes: the storage seam handles the file, and the column handles where
// it lives. Replacing an avatar returns the previous URL so the caller can remove the object it
// replaced — an avatar is the one upload a user will do repeatedly, and orphans accumulate fast.
package avatar

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound means the account is gone: a valid token for a deleted user.
var ErrNotFound = errors.New("avatar: user not found")

// Store is the persistence seam. Both writes return the URL they replaced, which is what lets the
// caller delete the object rather than leave it behind.
type Store interface {
	Set(ctx context.Context, userID, url string) (previous *string, err error)
	Clear(ctx context.Context, userID string) (previous *string, err error)
}

// Service applies the rule that there is nothing to it beyond the store: one avatar per account,
// because it is a column and not a collection.
type Service struct {
	store Store
}

// New wires the service.
func New(store Store) *Service { return &Service{store: store} }

// Set points the account at a new image and reports the one it replaced.
func (s *Service) Set(ctx context.Context, userID, url string) (*string, error) {
	previous, err := s.store.Set(ctx, userID, url)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("avatar: %w", err)
	}
	return previous, nil
}

// Clear removes the avatar and reports the URL it held.
func (s *Service) Clear(ctx context.Context, userID string) (*string, error) {
	previous, err := s.store.Clear(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("avatar: %w", err)
	}
	return previous, nil
}

// Postgres writes the column.
type Postgres struct {
	Pool *pgxpool.Pool
}

// NewPostgresStore returns a Store backed by the given pool.
func NewPostgresStore(pool *pgxpool.Pool) *Postgres { return &Postgres{Pool: pool} }

// Set updates the column and returns what it used to hold.
//
// The previous value travels out of the UPDATE itself (`FROM` a one-row snapshot) instead of being
// read first and written after: two round trips would let a concurrent upload interleave and leave
// an object nobody references.
func (p *Postgres) Set(ctx context.Context, userID, url string) (*string, error) {
	const query = `
UPDATE seg.tb_user u
   SET tx_avatar_url = $2, dt_updated_at = now()
  FROM (SELECT co_user, tx_avatar_url AS previous
          FROM seg.tb_user
         WHERE co_user = $1::uuid AND dt_deleted_at IS NULL) old
 WHERE u.co_user = old.co_user
RETURNING old.previous`

	var previous *string
	if err := p.Pool.QueryRow(ctx, query, userID, url).Scan(&previous); err != nil {
		return nil, ErrNotFound
	}
	return previous, nil
}

// Clear nulls the column and returns the URL it held.
func (p *Postgres) Clear(ctx context.Context, userID string) (*string, error) {
	const query = `
UPDATE seg.tb_user u
   SET tx_avatar_url = NULL, dt_updated_at = now()
  FROM (SELECT co_user, tx_avatar_url AS previous
          FROM seg.tb_user
         WHERE co_user = $1::uuid AND dt_deleted_at IS NULL) old
 WHERE u.co_user = old.co_user
RETURNING old.previous`

	var previous *string
	if err := p.Pool.QueryRow(ctx, query, userID).Scan(&previous); err != nil {
		return nil, ErrNotFound
	}
	return previous, nil
}
