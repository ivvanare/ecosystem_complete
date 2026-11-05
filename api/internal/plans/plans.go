// Package plans implements collaborative plans: an entity with a visibility door and a
// membership relation that carries the role (D7/D8).
package plans

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Visibility and role values mirror the enums created by ledger 25.
const (
	VisibilityPrivate = "PRIVATE"
	VisibilityPublic  = "PUBLIC"

	RoleOwner  = "OWNER"
	RoleEditor = "EDITOR"
	RoleViewer = "VIEWER"
)

// Errors the transport maps onto responses.
var (
	// ErrNotFound covers invisible, retired and missing alike. A private plan must not
	// confirm that it exists, so "not yours" and "not there" answer the same way.
	ErrNotFound = errors.New("plans: not found")
	// ErrForbidden is the visible-but-insufficient case: the caller may see the plan and
	// still may not do this. It is deliberately different from ErrNotFound.
	ErrForbidden = errors.New("plans: insufficient role")
)

// Plan is one plan as the API returns it.
type Plan struct {
	ID          string
	CreatorID   string
	Name        string
	Description *string
	Visibility  string
	Status      string
	ScheduledAt *time.Time
	PartySize   *int
	CreatedAt   time.Time
	UpdatedAt   time.Time
	// ViewerRole is the caller's role, empty for a public plan the caller has not joined.
	ViewerRole  string
	MemberCount int
}

// CreateInput is a new plan. Visibility defaults to PRIVATE when empty.
type CreateInput struct {
	Name        string
	Description *string
	Visibility  string
	ScheduledAt *time.Time
	PartySize   *int
}

// UpdateInput is a partial change: a nil field means "leave it".
type UpdateInput struct {
	Name        *string
	Description **string
	Visibility  *string
	ScheduledAt **time.Time
	PartySize   **int
}

// Store is the persistence seam; tests fake it, because the suite never needs PostgreSQL.
type Store interface {
	Create(ctx context.Context, actorID string, in CreateInput) (Plan, error)
	Get(ctx context.Context, actorID, planID string) (Plan, error)
	Role(ctx context.Context, actorID, planID string) (role string, visibility string, err error)
	Update(ctx context.Context, actorID, planID string, in UpdateInput) (Plan, error)
	Join(ctx context.Context, actorID, planID string, role string) error
	Remove(ctx context.Context, planID, userID string) error
	ListFor(ctx context.Context, actorID string, page, limit int) ([]Plan, int, error)
}

// Service applies the authorization rules.
type Service struct {
	store  Store
	logger *slog.Logger
}

// NewService wires the domain.
func NewService(store Store, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{store: store, logger: logger}
}

// Create inserts the plan and registers the creator as OWNER in the same transaction (S4).
func (s *Service) Create(ctx context.Context, actorID string, in CreateInput) (Plan, error) {
	if in.Visibility == "" {
		in.Visibility = VisibilityPrivate
	}
	if in.Visibility != VisibilityPrivate && in.Visibility != VisibilityPublic {
		return Plan{}, fmt.Errorf("plans: unknown visibility %q", in.Visibility)
	}
	return s.store.Create(ctx, actorID, in)
}

// Get returns the plan when the caller is a member or the plan is public (S6).
func (s *Service) Get(ctx context.Context, actorID, planID string) (Plan, error) {
	return s.store.Get(ctx, actorID, planID)
}

// Update changes a plan. VIEWER may read but not write (403); a stranger gets 404 (S7).
func (s *Service) Update(ctx context.Context, actorID, planID string, in UpdateInput) (Plan, error) {
	role, visibility, err := s.store.Role(ctx, actorID, planID)
	if err != nil {
		return Plan{}, err
	}
	if role == "" && visibility != VisibilityPublic {
		return Plan{}, ErrNotFound
	}
	if role != RoleOwner && role != RoleEditor {
		return Plan{}, ErrForbidden
	}
	// Visibility is the OWNER's alone: changing PRIVATE to PUBLIC touches everyone already
	// invited, and taking a public plan private hides it from people who joined.
	if in.Visibility != nil && role != RoleOwner {
		return Plan{}, ErrForbidden
	}
	if in.Visibility != nil && *in.Visibility != VisibilityPrivate && *in.Visibility != VisibilityPublic {
		return Plan{}, fmt.Errorf("plans: unknown visibility %q", *in.Visibility)
	}
	return s.store.Update(ctx, actorID, planID, in)
}

// Join is the self-join: PUBLIC plans only, and always as VIEWER (S5/D8).
func (s *Service) Join(ctx context.Context, actorID, planID string) error {
	role, visibility, err := s.store.Role(ctx, actorID, planID)
	if err != nil {
		return err
	}
	if visibility != VisibilityPublic {
		// A private plan does not confirm that it exists.
		return ErrNotFound
	}
	if role != "" {
		// Already a member: joining twice is idempotent and must not demote an EDITOR.
		return nil
	}
	return s.store.Join(ctx, actorID, planID, RoleViewer)
}

// Leave removes the caller, or someone else when an OWNER asks (S8).
func (s *Service) Leave(ctx context.Context, actorID, planID, targetID string) error {
	role, visibility, err := s.store.Role(ctx, actorID, planID)
	if err != nil {
		return err
	}
	callerIsMember := role != ""
	if !callerIsMember && visibility != VisibilityPublic {
		return ErrNotFound
	}

	switch {
	case targetID == actorID:
		if role == RoleOwner {
			// A plan without an owner is invalid: the owner hands over before leaving.
			return ErrForbidden
		}
		return s.store.Remove(ctx, planID, targetID)
	case role == RoleOwner:
		return s.store.Remove(ctx, planID, targetID)
	default:
		return ErrForbidden
	}
}

// List returns the plans the caller belongs to.
func (s *Service) List(ctx context.Context, actorID string, page, limit int) ([]Plan, int, error) {
	return s.store.ListFor(ctx, actorID, page, limit)
}

// PostgresStore is the pgx implementation.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore wires the store.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

const planColumns = `
SELECT r.co_route::text, r.co_user::text, r.tx_name, r.tx_description,
       r.tx_visibility::text, r.tx_status::text, r.dt_scheduled_at, r.nu_party_size,
       r.dt_created_at, r.dt_updated_at,
       coalesce((SELECT m.tx_role::text FROM core.tb_route_member m
                  WHERE m.co_route = r.co_route AND m.co_user = $1::uuid), ''),
       (SELECT count(*) FROM core.tb_route_member m2 WHERE m2.co_route = r.co_route)
FROM core.tb_route r`

// Create inserts the plan and its OWNER row in one transaction: a plan without an owner is
// invalid, so the two writes succeed or fail together (S4).
func (s *PostgresStore) Create(ctx context.Context, actorID string, in CreateInput) (Plan, error) {
	var planID string
	err := s.withActor(ctx, actorID, func(tx pgx.Tx) error {
		const insert = `
INSERT INTO core.tb_route
    (co_route, co_user, tx_name, tx_description, tx_visibility, dt_scheduled_at, nu_party_size,
     tx_status, dt_created_at, dt_updated_at)
VALUES (gen_random_uuid(), $1, $2, $3, $4::public.plan_visibility, $5, $6, 'ACTIVE', now(), now())
RETURNING co_route::text`
		if err := tx.QueryRow(ctx, insert, actorID, in.Name, in.Description, in.Visibility,
			in.ScheduledAt, in.PartySize).Scan(&planID); err != nil {
			return fmt.Errorf("inserting plan: %w", err)
		}
		const member = `
INSERT INTO core.tb_route_member (co_route, co_user, tx_role, dt_created_at, dt_updated_at)
VALUES ($1::uuid, $2::uuid, 'OWNER', now(), now())`
		if _, err := tx.Exec(ctx, member, planID, actorID); err != nil {
			return fmt.Errorf("registering the owner: %w", err)
		}
		return nil
	})
	if err != nil {
		return Plan{}, err
	}
	return s.Get(ctx, actorID, planID)
}

// Get returns a plan the caller may see: a member, or anyone when it is public.
func (s *PostgresStore) Get(ctx context.Context, actorID, planID string) (Plan, error) {
	row := s.pool.QueryRow(ctx, planColumns+`
WHERE r.co_route = $2::uuid
  AND r.dt_deleted_at IS NULL
  AND (r.tx_visibility = 'PUBLIC' OR EXISTS (
        SELECT 1 FROM core.tb_route_member m3
         WHERE m3.co_route = r.co_route AND m3.co_user = $1::uuid))`, actorID, planID)
	plan, err := scanPlan(row)
	if err != nil {
		return Plan{}, ErrNotFound
	}
	return plan, nil
}

// Role returns the caller's role ("" when not a member) and the plan's visibility.
func (s *PostgresStore) Role(ctx context.Context, actorID, planID string) (string, string, error) {
	const query = `
SELECT coalesce((SELECT m.tx_role::text FROM core.tb_route_member m
                  WHERE m.co_route = r.co_route AND m.co_user = $1::uuid), ''),
       r.tx_visibility::text
FROM core.tb_route r
WHERE r.co_route = $2::uuid AND r.dt_deleted_at IS NULL`
	var role, visibility string
	if err := s.pool.QueryRow(ctx, query, actorID, planID).Scan(&role, &visibility); err != nil {
		return "", "", ErrNotFound
	}
	return role, visibility, nil
}

// Update applies a partial change; authorization already happened in the service.
func (s *PostgresStore) Update(ctx context.Context, actorID, planID string, in UpdateInput) (Plan, error) {
	const query = `
UPDATE core.tb_route SET
    tx_name        = coalesce($2, tx_name),
    tx_description = CASE WHEN $3::boolean THEN $4 ELSE tx_description END,
    tx_visibility  = coalesce($5::public.plan_visibility, tx_visibility),
    dt_scheduled_at = CASE WHEN $6::boolean THEN $7 ELSE dt_scheduled_at END,
    nu_party_size  = CASE WHEN $8::boolean THEN $9 ELSE nu_party_size END,
    dt_updated_at  = now()
WHERE co_route = $1::uuid
RETURNING co_route::text`
	var touched string
	err := s.withActor(ctx, actorID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, query, planID, in.Name,
			in.Description != nil, derefString(in.Description),
			in.Visibility,
			in.ScheduledAt != nil, derefTime(in.ScheduledAt),
			in.PartySize != nil, derefInt(in.PartySize)).Scan(&touched)
	})
	if err != nil {
		return Plan{}, ErrNotFound
	}
	return s.Get(ctx, actorID, planID)
}

// Join inserts the membership with the role the service chose.
func (s *PostgresStore) Join(ctx context.Context, actorID, planID, role string) error {
	const query = `
INSERT INTO core.tb_route_member (co_route, co_user, tx_role, dt_created_at, dt_updated_at)
VALUES ($1::uuid, $2::uuid, $3::public.plan_role, now(), now())
ON CONFLICT (co_route, co_user) DO NOTHING`
	if _, err := s.pool.Exec(ctx, query, planID, actorID, role); err != nil {
		return fmt.Errorf("joining plan: %w", err)
	}
	return nil
}

// Remove deletes one membership; removing what is not there is not a failure.
func (s *PostgresStore) Remove(ctx context.Context, planID, userID string) error {
	const query = `DELETE FROM core.tb_route_member WHERE co_route = $1::uuid AND co_user = $2::uuid`
	if _, err := s.pool.Exec(ctx, query, planID, userID); err != nil {
		return fmt.Errorf("removing member: %w", err)
	}
	return nil
}

// ListFor returns the plans the caller belongs to, newest first.
func (s *PostgresStore) ListFor(ctx context.Context, actorID string, page, limit int) ([]Plan, int, error) {
	const count = `
SELECT count(*) FROM core.tb_route r
JOIN core.tb_route_member m ON m.co_route = r.co_route AND m.co_user = $1::uuid
WHERE r.dt_deleted_at IS NULL`
	var total int
	if err := s.pool.QueryRow(ctx, count, actorID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting plans: %w", err)
	}

	offset := (page - 1) * limit
	rows, err := s.pool.Query(ctx, planColumns+`
JOIN core.tb_route_member m4 ON m4.co_route = r.co_route AND m4.co_user = $1::uuid
WHERE r.dt_deleted_at IS NULL
ORDER BY r.dt_created_at DESC, r.co_route
LIMIT $2 OFFSET $3`, actorID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("listing plans: %w", err)
	}
	defer rows.Close()

	result := make([]Plan, 0, limit)
	for rows.Next() {
		plan, err := scanPlan(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scanning plan: %w", err)
		}
		result = append(result, plan)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("during plan iteration: %w", err)
	}
	return result, total, nil
}

// withActor runs one write with the caller announced, so any trigger observes the actor.
func (s *PostgresStore) withActor(ctx context.Context, actorID string, write func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT set_config('app.current_user_id', $1, true)`, actorID); err != nil {
		return fmt.Errorf("announcing the actor: %w", err)
	}
	if err := write(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func scanPlan(row interface{ Scan(dest ...any) error }) (Plan, error) {
	var plan Plan
	err := row.Scan(&plan.ID, &plan.CreatorID, &plan.Name, &plan.Description,
		&plan.Visibility, &plan.Status, &plan.ScheduledAt, &plan.PartySize,
		&plan.CreatedAt, &plan.UpdatedAt, &plan.ViewerRole, &plan.MemberCount)
	return plan, err
}

func derefString(value **string) any {
	if value == nil || *value == nil {
		return nil
	}
	return **value
}

func derefTime(value **time.Time) any {
	if value == nil || *value == nil {
		return nil
	}
	return **value
}

func derefInt(value **int) any {
	if value == nil || *value == nil {
		return nil
	}
	return **value
}
