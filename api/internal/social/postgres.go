package social

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	uniqueViolation  = "23505"
	emailUniqueIndex = "ux_tb_user_email_active"
)

// PostgresStore links provider subjects to users.
type PostgresStore struct {
	Pool *pgxpool.Pool
}

// NewPostgresStore returns a Store backed by the given pool.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{Pool: pool}
}

// UserIDByIdentity resolves the account by (provider, subject).
func (s *PostgresStore) UserIDByIdentity(ctx context.Context, provider, subject string) (string, error) {
	var userID string
	err := s.Pool.QueryRow(ctx, `
		SELECT co_user::text FROM seg.tb_user_identity
		WHERE tx_provider = $1::identity_provider
		  AND tx_provider_user_id = $2
		  AND dt_deleted_at IS NULL`, provider, subject).Scan(&userID)

	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoIdentity
	}
	if err != nil {
		return "", fmt.Errorf("selecting identity: %w", err)
	}
	return userID, nil
}

// CreateUserWithIdentity creates the account and its link in one transaction.
//
// An unverified provider address is never stored: keeping it out of tx_email means it cannot
// occupy the unique index and block the person who really owns that address.
func (s *PostgresStore) CreateUserWithIdentity(ctx context.Context, identity Identity) (string, error) {
	email := identity.Email
	if !identity.EmailVerified {
		email = nil
	}

	userID, err := s.create(ctx, identity, email)
	if err != nil {
		// The verified address already belongs to another account. Create the federated
		// account without it rather than linking the two: linking by email would let a
		// provider take over an existing account.
		var pgErr *pgconn.PgError
		if email != nil && errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == emailUniqueIndex {
			return s.create(ctx, identity, nil)
		}
		return "", err
	}
	return userID, nil
}

func (s *PostgresStore) create(ctx context.Context, identity Identity, email *string) (string, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("beginning transaction: %w", err)
	}
	settled := false
	defer func() {
		if !settled {
			_ = tx.Rollback(ctx)
		}
	}()

	var userID string
	err = tx.QueryRow(ctx, `
		INSERT INTO seg.tb_user
			(co_user, tx_name, tx_email, tx_password_hash, dt_email_verified_at,
			 tx_status, dt_created_at, dt_updated_at)
		VALUES (gen_random_uuid(), $1, $2, NULL,
		        CASE WHEN $3 THEN now() ELSE NULL END, 'ACTIVE', now(), now())
		RETURNING co_user::text`,
		identity.Name, email, identity.EmailVerified && email != nil).Scan(&userID)
	if err != nil {
		return "", fmt.Errorf("inserting tb_user: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO seg.tb_user_identity
			(co_user_identity, co_user, tx_provider, tx_provider_user_id, tx_email,
			 dt_linked_at, dt_created_at, dt_updated_at)
		VALUES (gen_random_uuid(), $1::uuid, $2::identity_provider, $3, $4, now(), now(), now())`,
		userID, identity.Provider, identity.Subject, identity.Email); err != nil {
		return "", fmt.Errorf("inserting tb_user_identity: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("committing: %w", err)
	}
	settled = true
	return userID, nil
}
