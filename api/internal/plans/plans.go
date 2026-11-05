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

	"github.com/ivvanare/ecosystem_complete/api/internal/invitations"
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

// PlanPlace is one row of the ordered itinerary.
type PlanPlace struct {
	PlaceID   string
	Title     string
	Order     int
	NetVotes  int
	MyVote    int // 0 when the caller has not voted
	CreatedAt time.Time
}

// AdminFilter narrows the moderation listing. Every field is optional.
type AdminFilter struct {
	Status     string
	OwnerID    string
	Visibility string
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
	AddPlace(ctx context.Context, actorID, planID, placeID string, order int) error
	RemovePlace(ctx context.Context, planID, placeID string) error
	Places(ctx context.Context, actorID, planID string) ([]PlanPlace, error)
	Vote(ctx context.Context, actorID, planID, placeID string, value int) error
	AdminList(ctx context.Context, filter AdminFilter, page, limit int) ([]Plan, int, error)
	SetStatus(ctx context.Context, actorID, planID, status string) error
}

// Service applies the authorization rules.
// Notifier announces an event. It is a courtesy, so a failure never blocks the act.
type Notifier interface {
	InvitationReceived(ctx context.Context, userID, planName string)
}

type Service struct {
	store       Store
	logger      *slog.Logger
	invitations Invitations
	notifier    Notifier
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

// Places returns the ordered itinerary: net votes descending, ties by the manual order, then by
// creation time, so the result is deterministic and reproducible (D9).
func (s *Service) Places(ctx context.Context, actorID, planID string) ([]PlanPlace, error) {
	role, visibility, err := s.store.Role(ctx, actorID, planID)
	if err != nil {
		return nil, err
	}
	if role == "" && visibility != VisibilityPublic {
		return nil, ErrNotFound
	}
	// A guest invited in SURPRISE mode has not seen the venue yet: the itinerary is the venue
	// (S26/D20). A member is already inside the plan and is never hidden from.
	if role == "" && s.invitations != nil {
		hidden, err := s.invitations.VenueHidden(ctx, actorID, planID)
		if err != nil {
			return nil, err
		}
		if hidden {
			return nil, ErrForbidden
		}
	}
	return s.store.Places(ctx, actorID, planID)
}

// AddPlace adds a place to the plan. EDITOR or OWNER only: a VIEWER reads and votes, it does
// not shape the itinerary (S7).
func (s *Service) AddPlace(ctx context.Context, actorID, planID, placeID string, order int) error {
	role, visibility, err := s.store.Role(ctx, actorID, planID)
	if err != nil {
		return err
	}
	if role == "" && visibility != VisibilityPublic {
		return ErrNotFound
	}
	if role != RoleOwner && role != RoleEditor {
		return ErrForbidden
	}
	return s.store.AddPlace(ctx, actorID, planID, placeID, order)
}

// RemovePlace removes a place, and its votes with it (the composite FK cascades).
func (s *Service) RemovePlace(ctx context.Context, actorID, planID, placeID string) error {
	role, visibility, err := s.store.Role(ctx, actorID, planID)
	if err != nil {
		return err
	}
	if role == "" && visibility != VisibilityPublic {
		return ErrNotFound
	}
	if role != RoleOwner && role != RoleEditor {
		return ErrForbidden
	}
	return s.store.RemovePlace(ctx, planID, placeID)
}

// Vote records the caller's vote on a place in the plan.
//
// Only a member may vote — a vote from a non-member is rejected, never silently dropped (S9).
// A public plan does not let a passer-by vote: reading is open, shaping is not.
func (s *Service) Vote(ctx context.Context, actorID, planID, placeID string, value int) error {
	if value != 1 && value != -1 {
		return fmt.Errorf("plans: vote must be 1 or -1")
	}
	role, visibility, err := s.store.Role(ctx, actorID, planID)
	if err != nil {
		return err
	}
	if role == "" && visibility != VisibilityPublic {
		return ErrNotFound
	}
	if role == "" {
		return ErrForbidden
	}
	return s.store.Vote(ctx, actorID, planID, placeID, value)
}

// AdminList is the moderation listing: every plan, regardless of membership.
func (s *Service) AdminList(ctx context.Context, filter AdminFilter, page, limit int) ([]Plan, int, error) {
	return s.store.AdminList(ctx, filter, page, limit)
}

// Moderate takes a plan out of circulation, or puts it back. It is not participation: the
// moderator never joins, adds places or votes (D21).
func (s *Service) Moderate(ctx context.Context, actorID, planID, status string) error {
	if status != "ACTIVE" && status != "INACTIVE" {
		return fmt.Errorf("plans: unknown status %q", status)
	}
	return s.store.SetStatus(ctx, actorID, planID, status)
}

// List returns the plans the caller belongs to.
func (s *Service) List(ctx context.Context, actorID string, page, limit int) ([]Plan, int, error) {
	return s.store.ListFor(ctx, actorID, page, limit)
}

// Invitations is the seam the plan routes use for the invitation lifecycle. It is an interface
// so the plan service does not depend on the invitations package's concrete store.
type Invitations interface {
	Create(ctx context.Context, actorID, planID string, in invitations.CreateInput) (invitations.Invitation, error)
	ForPlan(ctx context.Context, actorID, planID string) ([]invitations.Invitation, error)
	Mine(ctx context.Context, userID string) ([]invitations.Invitation, error)
	Accept(ctx context.Context, actorID, invitationID string) error
	Decline(ctx context.Context, actorID, invitationID string) error
	// VenueHidden reports whether an unrevealed SURPRISE invitation still hides the venue.
	VenueHidden(ctx context.Context, userID, planID string) (bool, error)
	AdminList(ctx context.Context, filter invitations.AdminFilter, page, limit int) ([]invitations.Invitation, int, error)
	Revoke(ctx context.Context, actorID, invitationID string) error
}

// SetNotifier wires the courtesy channel.
func (s *Service) SetNotifier(notifier Notifier) { s.notifier = notifier }

// SetInvitations wires the invitation lifecycle into the plan surface.
func (s *Service) SetInvitations(service Invitations) { s.invitations = service }

// Invite delegates to the invitation lifecycle.
func (s *Service) Invite(ctx context.Context, actorID, planID string, in invitations.CreateInput) (invitations.Invitation, error) {
	created, err := s.invitations.Create(ctx, actorID, planID, in)
	if err != nil {
		return invitations.Invitation{}, err
	}
	// The invitation already exists; a courtesy that fails must not undo it.
	if s.notifier != nil {
		planName := planID
		if created.PlanName != nil && *created.PlanName != "" {
			planName = *created.PlanName
		}
		s.notifier.InvitationReceived(ctx, created.UserID, planName)
	}
	return created, nil
}

// InvitationAdmins is the moderation listing of every invitation in the site.
func (s *Service) InvitationAdmins(ctx context.Context, filter invitations.AdminFilter, page, limit int) ([]invitations.Invitation, int, error) {
	return s.invitations.AdminList(ctx, filter, page, limit)
}

// RevokeInvitation cancels an invitation without becoming a participant.
func (s *Service) RevokeInvitation(ctx context.Context, actorID, invitationID string) error {
	return s.invitations.Revoke(ctx, actorID, invitationID)
}

// PlanInvitations is the host's RSVP view.
func (s *Service) PlanInvitations(ctx context.Context, actorID, planID string) ([]invitations.Invitation, error) {
	return s.invitations.ForPlan(ctx, actorID, planID)
}

// MyInvitations is the caller's own inbox.
func (s *Service) MyInvitations(ctx context.Context, userID string) ([]invitations.Invitation, error) {
	return s.invitations.Mine(ctx, userID)
}

// AcceptInvitation answers an invitation with a yes.
func (s *Service) AcceptInvitation(ctx context.Context, actorID, invitationID string) error {
	return s.invitations.Accept(ctx, actorID, invitationID)
}

// DeclineInvitation answers with a no; it creates nothing.
func (s *Service) DeclineInvitation(ctx context.Context, actorID, invitationID string) error {
	return s.invitations.Decline(ctx, actorID, invitationID)
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

// adminPlanColumns is the moderation projection. It deliberately omits the viewer-role
// subquery: the moderator is not a participant, and reusing planColumns would bind the
// filter arguments to $1 where the subquery expects the viewer id.
const adminPlanColumns = `
SELECT r.co_route::text, r.co_user::text, r.tx_name, r.tx_description,
       r.tx_visibility::text, r.tx_status::text, r.dt_scheduled_at, r.nu_party_size,
       r.dt_created_at, r.dt_updated_at,
       '' AS viewer_role,
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
  -- A suspended plan stops being visible to non-members; members keep it so nobody loses
  -- their itinerary (S8). Without this clause suspending a plan did nothing on read.
  AND (r.tx_status = 'ACTIVE' OR EXISTS (
        SELECT 1 FROM core.tb_route_member m6
         WHERE m6.co_route = r.co_route AND m6.co_user = $1::uuid))
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

// AddPlace inserts or reorders a place in the itinerary. The composite primary key makes this
// an upsert on order rather than a duplicate.
func (s *PostgresStore) AddPlace(ctx context.Context, actorID, planID, placeID string, order int) error {
	const query = `
INSERT INTO core.tb_route_place (co_route, co_place, nu_order, dt_created_at, dt_updated_at)
VALUES ($1::uuid, $2::uuid, $3, now(), now())
ON CONFLICT (co_route, co_place) DO UPDATE
   SET nu_order = EXCLUDED.nu_order, dt_updated_at = now()`
	if _, err := s.pool.Exec(ctx, query, planID, placeID, order); err != nil {
		return fmt.Errorf("adding place to plan: %w", err)
	}
	return nil
}

// RemovePlace deletes the row; its votes go with it through the composite FK.
func (s *PostgresStore) RemovePlace(ctx context.Context, planID, placeID string) error {
	const query = `DELETE FROM core.tb_route_place WHERE co_route = $1::uuid AND co_place = $2::uuid`
	if _, err := s.pool.Exec(ctx, query, planID, placeID); err != nil {
		return fmt.Errorf("removing place from plan: %w", err)
	}
	return nil
}

// Places reads the itinerary ordered by net votes, then the manual order, then creation time.
//
// The aggregate is over one plan's rows, not over the review table, so it is a bounded sum of a
// small set — the interactive read this endpoint exists for.
func (s *PostgresStore) Places(ctx context.Context, actorID, planID string) ([]PlanPlace, error) {
	const query = `
SELECT rp.co_place::text, p.tx_title, rp.nu_order,
       coalesce(sum(v.nu_vote), 0)::int AS net_votes,
       -- The plan id comes from the parameter, not from the grouped row: a correlated
       -- reference to rp.co_route would be an ungrouped column and Postgres rejects it.
       coalesce((SELECT vv.nu_vote FROM core.tb_route_place_vote vv
                  WHERE vv.co_route = $2::uuid AND vv.co_place = rp.co_place
                    AND vv.co_user = $1::uuid), 0)::int AS my_vote,
       rp.dt_created_at
FROM core.tb_route_place rp
JOIN core.tb_place p ON p.co_place = rp.co_place
LEFT JOIN core.tb_route_place_vote v
       ON v.co_route = rp.co_route AND v.co_place = rp.co_place
WHERE rp.co_route = $2::uuid
GROUP BY rp.co_place, p.tx_title, rp.nu_order, rp.dt_created_at
ORDER BY net_votes DESC, rp.nu_order ASC, rp.dt_created_at ASC`
	rows, err := s.pool.Query(ctx, query, actorID, planID)
	if err != nil {
		return nil, fmt.Errorf("reading plan places: %w", err)
	}
	defer rows.Close()

	result := make([]PlanPlace, 0, 8)
	for rows.Next() {
		var place PlanPlace
		if err := rows.Scan(&place.PlaceID, &place.Title, &place.Order,
			&place.NetVotes, &place.MyVote, &place.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning plan place: %w", err)
		}
		result = append(result, place)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("during plan place iteration: %w", err)
	}
	return result, nil
}

// Vote upserts the caller's vote: changing your mind is an update, not a second row.
func (s *PostgresStore) Vote(ctx context.Context, actorID, planID, placeID string, value int) error {
	const query = `
INSERT INTO core.tb_route_place_vote
    (co_route, co_place, co_user, nu_vote, dt_created_at, dt_updated_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, now(), now())
ON CONFLICT (co_route, co_place, co_user) DO UPDATE
   SET nu_vote = EXCLUDED.nu_vote, dt_updated_at = now()`
	if _, err := s.pool.Exec(ctx, query, planID, placeID, actorID, value); err != nil {
		return fmt.Errorf("voting: %w", err)
	}
	return nil
}

// AdminList returns one page of every plan, newest first, with the optional filters.
func (s *PostgresStore) AdminList(ctx context.Context, filter AdminFilter, page, limit int) ([]Plan, int, error) {
	const predicate = `
WHERE r.dt_deleted_at IS NULL
  AND ($1::public.activatable_status IS NULL OR r.tx_status = $1::public.activatable_status)
  AND ($2::uuid IS NULL OR r.co_user = $2::uuid)
  AND ($3::public.plan_visibility IS NULL OR r.tx_visibility = $3::public.plan_visibility)`

	args := []any{nullableText(filter.Status), nullableUUID(filter.OwnerID), nullableText(filter.Visibility)}

	var total int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM core.tb_route r"+predicate, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting plans for moderation: %w", err)
	}

	offset := (page - 1) * limit
	rows, err := s.pool.Query(ctx, adminPlanColumns+predicate+`
ORDER BY r.dt_created_at DESC, r.co_route
LIMIT $4 OFFSET $5`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("listing plans for moderation: %w", err)
	}
	defer rows.Close()

	result := make([]Plan, 0, limit)
	for rows.Next() {
		plan, err := scanPlan(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scanning plan for moderation: %w", err)
		}
		result = append(result, plan)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("during moderation plan iteration: %w", err)
	}
	return result, total, nil
}

// SetStatus suspends or restores a plan. The actor is announced for any observer of the write.
func (s *PostgresStore) SetStatus(ctx context.Context, actorID, planID, status string) error {
	const query = `
UPDATE core.tb_route SET tx_status = $2::public.activatable_status, dt_updated_at = now()
WHERE co_route = $1::uuid AND dt_deleted_at IS NULL`
	return s.withActor(ctx, actorID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, query, planID, status); err != nil {
			return fmt.Errorf("setting plan status: %w", err)
		}
		return nil
	})
}

// nullableText binds an absent filter as NULL, which the predicates read as "no filter".
func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// nullableUUID binds an absent id as NULL, because "" is an invalid cast for uuid.
func nullableUUID(value string) any {
	if value == "" {
		return nil
	}
	return value
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
