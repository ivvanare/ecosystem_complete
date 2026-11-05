// Package follow implements the asymmetric social graph: one direction per row, and
// "friend" computed as a mutual follow at read time (D6, S5).
package follow

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Errors the transport maps onto responses. Following twice is not an error: the graph is
// a set, so the operation is idempotent and the answer is the same either way.
var (
	// ErrSelfFollow is a validated failure, not a permission one: a person may not
	// follow themselves, and the database says so too (ck_tb_follow_not_self).
	ErrSelfFollow = errors.New("cannot follow yourself")
	// ErrUnknownUser is returned when the target does not exist or is not active, so a
	// caller cannot use the endpoint to probe which addresses have accounts.
	ErrUnknownUser = errors.New("user not found")
)

// Direction selects which side of the graph a listing reads.
const (
	DirectionFollowing = "following"
	DirectionFollowers = "followers"
)

// Member is one row of a follow listing: the public shape of another account.
type Member struct {
	UserID string
	Name   string
	// Mutual marks a "friend": the other side of the edge exists too. It is computed,
	// never stored, so it cannot rot when the graph changes (S2).
	Mutual bool
}

// Page is one page of a listing.
type Page struct {
	Members []Member
	Total   int
	Page    int
	Limit   int
}

// Store is the persistence seam; tests fake it, because the suite never needs PostgreSQL.
type Store interface {
	Follow(ctx context.Context, follower, followed string, at time.Time) error
	Unfollow(ctx context.Context, follower, followed string) error
	UserExists(ctx context.Context, userID string) (bool, error)
	List(ctx context.Context, viewerID, direction string, page, limit int) (Page, error)
}

// Service is the graph's domain surface.
type Service struct {
	store  Store
	logger *slog.Logger
}

// NewService wires the graph.
func NewService(store Store, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{store: store, logger: logger}
}

// Follow creates the edge. It is idempotent: repeating it leaves the graph unchanged and
// reports success, because a set does not care how many times you add the same element.
func (s *Service) Follow(ctx context.Context, follower, followed string) error {
	if follower == followed {
		return ErrSelfFollow
	}
	exists, err := s.store.UserExists(ctx, followed)
	if err != nil {
		return fmt.Errorf("checking target user: %w", err)
	}
	if !exists {
		return ErrUnknownUser
	}
	if err := s.store.Follow(ctx, follower, followed, time.Now().UTC()); err != nil {
		return fmt.Errorf("following user: %w", err)
	}
	return nil
}

// Unfollow removes the edge, idempotently: removing what is not there is a success.
func (s *Service) Unfollow(ctx context.Context, follower, followed string) error {
	if follower == followed {
		return ErrSelfFollow
	}
	if err := s.store.Unfollow(ctx, follower, followed); err != nil {
		return fmt.Errorf("unfollowing user: %w", err)
	}
	return nil
}

// List returns one page of the viewer's graph in the requested direction.
func (s *Service) List(ctx context.Context, viewerID, direction string, page, limit int) (Page, error) {
	if direction == "" {
		direction = DirectionFollowing
	}
	if direction != DirectionFollowing && direction != DirectionFollowers {
		return Page{}, fmt.Errorf("unknown direction %q", direction)
	}
	return s.store.List(ctx, viewerID, direction, page, limit)
}

// PostgresStore is the pgx implementation.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore wires the store.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

// Follow inserts the edge, ignoring a repeat: the composite primary key makes the
// operation a set insertion rather than an error path.
func (s *PostgresStore) Follow(ctx context.Context, follower, followed string, at time.Time) error {
	const query = `
INSERT INTO core.tb_follow (co_follower, co_followed, dt_created_at)
VALUES ($1, $2, $3)
ON CONFLICT (co_follower, co_followed) DO NOTHING`
	if _, err := s.pool.Exec(ctx, query, follower, followed, at); err != nil {
		return fmt.Errorf("inserting follow: %w", err)
	}
	return nil
}

// Unfollow deletes the edge; deleting nothing is not a failure.
func (s *PostgresStore) Unfollow(ctx context.Context, follower, followed string) error {
	const query = `DELETE FROM core.tb_follow WHERE co_follower = $1 AND co_followed = $2`
	if _, err := s.pool.Exec(ctx, query, follower, followed); err != nil {
		return fmt.Errorf("deleting follow: %w", err)
	}
	return nil
}

// UserExists reports whether the target is a live account.
func (s *PostgresStore) UserExists(ctx context.Context, userID string) (bool, error) {
	const query = `SELECT EXISTS (SELECT 1 FROM seg.tb_user WHERE co_user = $1 AND dt_deleted_at IS NULL)`
	var exists bool
	if err := s.pool.QueryRow(ctx, query, userID).Scan(&exists); err != nil {
		return false, fmt.Errorf("checking user: %w", err)
	}
	return exists, nil
}

// listFollowing reads the accounts the viewer follows.
//
// Mutual is computed with a correlated EXISTS rather than a stored flag (S2): the same row
// is a friend for one viewer and a stranger for another.
const listFollowing = `
SELECT u.co_user::text, u.tx_name,
       EXISTS (SELECT 1 FROM core.tb_follow back
                WHERE back.co_follower = f.co_followed AND back.co_followed = f.co_follower) AS mutual
FROM core.tb_follow f
JOIN seg.tb_user u ON u.co_user = f.co_followed
WHERE f.co_follower = $1 AND u.dt_deleted_at IS NULL
ORDER BY f.dt_created_at DESC, u.co_user
LIMIT $2 OFFSET $3`

// listFollowers reads the accounts that follow the viewer.
const listFollowers = `
SELECT u.co_user::text, u.tx_name,
       EXISTS (SELECT 1 FROM core.tb_follow back
                WHERE back.co_follower = f.co_follower AND back.co_followed = f.co_followed) AS mutual
FROM core.tb_follow f
JOIN seg.tb_user u ON u.co_user = f.co_follower
WHERE f.co_followed = $1 AND u.dt_deleted_at IS NULL
ORDER BY f.dt_created_at DESC, u.co_user
LIMIT $2 OFFSET $3`

const countFollowing = `SELECT count(*) FROM core.tb_follow WHERE co_follower = $1`
const countFollowers = `SELECT count(*) FROM core.tb_follow WHERE co_followed = $1`

// List returns one page plus the total in that direction.
func (s *PostgresStore) List(ctx context.Context, viewerID, direction string, page, limit int) (Page, error) {
	query, countQuery := listFollowing, countFollowing
	if direction == DirectionFollowers {
		query, countQuery = listFollowers, countFollowers
	}

	var total int
	if err := s.pool.QueryRow(ctx, countQuery, viewerID).Scan(&total); err != nil {
		return Page{}, fmt.Errorf("counting graph: %w", err)
	}

	offset := (page - 1) * limit
	rows, err := s.pool.Query(ctx, query, viewerID, limit, offset)
	if err != nil {
		return Page{}, fmt.Errorf("listing graph: %w", err)
	}
	defer rows.Close()

	members := make([]Member, 0, limit)
	for rows.Next() {
		var member Member
		if err := rows.Scan(&member.UserID, &member.Name, &member.Mutual); err != nil {
			return Page{}, fmt.Errorf("scanning graph member: %w", err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("during graph iteration: %w", err)
	}
	return Page{Members: members, Total: total, Page: page, Limit: limit}, nil
}

// Compile-time guards: the pgx row interface is satisfied by the pool.
var _ = pgx.ErrNoRows
