// Package blocks implements user blocking: a relation that applies in both directions and
// gates every viewer-relative projection (decision Q2).
package blocks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrSelfBlock is a validated failure: a person may not block themselves, and the database
// says so too.
var ErrSelfBlock = errors.New("blocks: cannot block yourself")

// Block is one row of the caller's block list.
type Block struct {
	UserID    string
	Name      *string
	CreatedAt time.Time
}

// Store is the persistence seam; tests fake it, because the suite never needs PostgreSQL.
type Store interface {
	Block(ctx context.Context, blocker, blocked string, at time.Time) error
	Unblock(ctx context.Context, blocker, blocked string) error
	List(ctx context.Context, blocker string) ([]Block, error)
	UserExists(ctx context.Context, userID string) (bool, error)
}

// Service is the domain surface.
type Service struct {
	store  Store
	logger *slog.Logger
	now    func() time.Time
}

// NewService wires the domain.
func NewService(store Store, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{store: store, logger: logger, now: func() time.Time { return time.Now().UTC() }}
}

// Block creates the relation. Idempotent: blocking twice leaves one row.
func (s *Service) Block(ctx context.Context, blocker, blocked string) error {
	if blocker == blocked {
		return ErrSelfBlock
	}
	exists, err := s.store.UserExists(ctx, blocked)
	if err != nil {
		return fmt.Errorf("checking the target user: %w", err)
	}
	if !exists {
		return fmt.Errorf("blocks: unknown user")
	}
	return s.store.Block(ctx, blocker, blocked, s.now())
}

// Unblock removes the relation; removing what is not there is not a failure.
func (s *Service) Unblock(ctx context.Context, blocker, blocked string) error {
	if blocker == blocked {
		return ErrSelfBlock
	}
	return s.store.Unblock(ctx, blocker, blocked)
}

// List returns the caller's block list.
func (s *Service) List(ctx context.Context, blocker string) ([]Block, error) {
	return s.store.List(ctx, blocker)
}

// PostgresStore is the pgx implementation.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore wires the store.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

// Block inserts the relation, ignoring a repeat: the composite primary key makes it a set
// insertion rather than an error path.
func (s *PostgresStore) Block(ctx context.Context, blocker, blocked string, at time.Time) error {
	const query = `
INSERT INTO core.tb_block (co_blocker, co_blocked, dt_created_at)
VALUES ($1::uuid, $2::uuid, $3)
ON CONFLICT (co_blocker, co_blocked) DO NOTHING`
	if _, err := s.pool.Exec(ctx, query, blocker, blocked, at); err != nil {
		return fmt.Errorf("blocking user: %w", err)
	}
	return nil
}

// Unblock deletes the relation; deleting nothing is not a failure.
func (s *PostgresStore) Unblock(ctx context.Context, blocker, blocked string) error {
	const query = `DELETE FROM core.tb_block WHERE co_blocker = $1::uuid AND co_blocked = $2::uuid`
	if _, err := s.pool.Exec(ctx, query, blocker, blocked); err != nil {
		return fmt.Errorf("unblocking user: %w", err)
	}
	return nil
}

// List returns whom the caller blocks.
func (s *PostgresStore) List(ctx context.Context, blocker string) ([]Block, error) {
	const query = `
SELECT u.co_user::text, u.tx_name, b.dt_created_at
FROM core.tb_block b
JOIN seg.tb_user u ON u.co_user = b.co_blocked
WHERE b.co_blocker = $1::uuid
ORDER BY b.dt_created_at DESC`
	rows, err := s.pool.Query(ctx, query, blocker)
	if err != nil {
		return nil, fmt.Errorf("listing blocks: %w", err)
	}
	defer rows.Close()

	result := make([]Block, 0, 8)
	for rows.Next() {
		var block Block
		if err := rows.Scan(&block.UserID, &block.Name, &block.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning block: %w", err)
		}
		result = append(result, block)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("during block iteration: %w", err)
	}
	return result, nil
}

// UserExists reports whether the target is a live account.
func (s *PostgresStore) UserExists(ctx context.Context, userID string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM seg.tb_user WHERE co_user = $1::uuid AND dt_deleted_at IS NULL)`,
		userID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("checking user: %w", err)
	}
	return exists, nil
}
