// Package invitations implements plan invitations: a lifecycle with a read-enforced expiry,
// and an accept path that grants the least privilege (S19/D16).
package invitations

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Status values mirror public.plan_invitation_status.
const (
	StatusPending  = "PENDING"
	StatusAccepted = "ACCEPTED"
	StatusDeclined = "DECLINED"
	StatusExpired  = "EXPIRED"
	StatusRevoked  = "REVOKED"
)

// Mode values mirror public.plan_invitation_mode.
const (
	ModePlain    = "PLAIN"
	ModeSurprise = "SURPRISE"
)

// Errors the transport maps onto responses.
var (
	// ErrNotFound covers invisible, expired and missing alike. An invitation the caller may
	// not see must not confirm that it exists, and an expired one is not acceptable.
	ErrNotFound = errors.New("invitations: not found")
	// ErrForbidden is the visible-but-insufficient case.
	ErrForbidden = errors.New("invitations: insufficient role")
	// ErrNotMember is a plan the caller may see but may not invite into.
	ErrNotMember = errors.New("invitations: the caller is not a member of the plan")
	// ErrNotInvitee is a caller trying to answer someone else's invitation.
	ErrNotInvitee = errors.New("invitations: the caller is not the invitee")
	// ErrExpiryNotFuture is a validated failure, not a server error: an invitation that has
	// already expired cannot be sent.
	ErrExpiryNotFuture = errors.New("invitations: the expiry must be in the future")
	// ErrSurpriseNeedsContent: a SURPRISE invitation without a message or a hint is an empty
	// envelope, and an empty envelope is not a surprise (S26).
	ErrSurpriseNeedsContent = errors.New("invitations: a surprise needs a message or a hint")
	// ErrUnknownMode is a validated failure about the closed mode set.
	ErrUnknownMode = errors.New("invitations: unknown mode")
	// ErrAlreadyPending is the partial unique index telling the truth: this person already has
	// a live invitation for this plan. Surfacing it as a conflict is the only honest answer —
	// the alternative is a 500 for a situation a client can resolve by waiting or revoking.
	ErrAlreadyPending = errors.New("invitations: a live invitation already exists for this person and plan")
)

// Invitation is one invitation as the API returns it.
type Invitation struct {
	ID          string
	PlanID      string
	UserID      string
	InviterID   string
	Status      string
	Mode        string
	Message     *string
	Hint        *string
	ExpiresAt   time.Time
	CreatedAt   time.Time
	RespondedAt *time.Time
	// InviteeName is the display name, for the host's view.
	InviteeName *string
	// PlanName is the plan title, for the invitee's view.
	PlanName *string
	PlanAt   *time.Time
	// Revealed reports whether the venue may be shown to the invitee (S26/D20). A PLAIN
	// invitation never hides anything; a SURPRISE one hides the venue until its rule fires.
	Revealed bool
	// Expired reports the read predicate's verdict: a PENDING row past its expiry is already
	// unusable, whether or not a sweeper has run (S2).
	Expired bool
}

// CreateInput is a new invitation.
type CreateInput struct {
	UserID    string
	ExpiresAt time.Time
	Mode      string
	Message   *string
	Hint      *string
}

// Store is the persistence seam; tests fake it, because the suite never needs PostgreSQL.
type Store interface {
	// Role returns the caller's role in the plan and its visibility, or "" when not a member.
	Role(ctx context.Context, actorID, planID string) (role, visibility string, err error)
	PlanVisible(ctx context.Context, actorID, planID string) (bool, error)
	Create(ctx context.Context, actorID, planID string, in CreateInput) (Invitation, error)
	ListForPlan(ctx context.Context, planID string) ([]Invitation, error)
	ListForUser(ctx context.Context, userID string) ([]Invitation, error)
	// VenueHidden reports whether an unrevealed SURPRISE invitation still hides the venue from
	// this caller in this plan.
	VenueHidden(ctx context.Context, userID, planID string) (bool, error)
	// Expire flips the stale PENDING rows to EXPIRED. It is hygiene, not enforcement: the read
	// predicate already treats those rows as unusable.
	Expire(ctx context.Context, now time.Time) (int64, error)
	// Answer flips a live invitation and, when accepting, inserts the VIEWER membership in the
	// same transaction.
	Answer(ctx context.Context, actorID, invitationID, status string, at time.Time) error
}

// Service applies the rules.
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

// Create invites a registered person. Only EDITOR or OWNER of the plan may invite (S3).
func (s *Service) Create(ctx context.Context, actorID, planID string, in CreateInput) (Invitation, error) {
	role, visibility, err := s.store.Role(ctx, actorID, planID)
	if err != nil {
		return Invitation{}, err
	}
	if role == "" && visibility == "" {
		return Invitation{}, ErrNotFound
	}
	if role != "OWNER" && role != "EDITOR" {
		return Invitation{}, ErrForbidden
	}
	if in.UserID == "" {
		return Invitation{}, fmt.Errorf("invitations: a target user is required")
	}
	if in.Mode == "" {
		in.Mode = ModePlain
	}
	if in.Mode != ModePlain && in.Mode != ModeSurprise {
		return Invitation{}, ErrUnknownMode
	}
	// A SURPRISE invitation needs content: an empty envelope is not a surprise (S26).
	if in.Mode == ModeSurprise && isEmpty(in.Message) && isEmpty(in.Hint) {
		return Invitation{}, ErrSurpriseNeedsContent
	}
	if !in.ExpiresAt.After(s.now()) {
		return Invitation{}, ErrExpiryNotFuture
	}
	return s.store.Create(ctx, actorID, planID, in)
}

// ForPlan is the host's RSVP view (S5): every invitee's status.
func (s *Service) ForPlan(ctx context.Context, actorID, planID string) ([]Invitation, error) {
	role, _, err := s.store.Role(ctx, actorID, planID)
	if err != nil {
		return nil, err
	}
	if role != "OWNER" && role != "EDITOR" {
		return nil, ErrForbidden
	}
	return s.store.ListForPlan(ctx, planID)
}

// VenueHidden delegates the reveal rule to the store, which owns the projection.
func (s *Service) VenueHidden(ctx context.Context, userID, planID string) (bool, error) {
	return s.store.VenueHidden(ctx, userID, planID)
}

// Mine is the caller's own invitations — the only ones a guest may see (S5).
func (s *Service) Mine(ctx context.Context, userID string) ([]Invitation, error) {
	return s.store.ListForUser(ctx, userID)
}

// Accept flips the invitation and creates the membership as VIEWER, in one transaction (S4).
func (s *Service) Accept(ctx context.Context, actorID, invitationID string) error {
	return s.answer(ctx, actorID, invitationID, StatusAccepted)
}

// Decline is terminal and creates nothing.
func (s *Service) Decline(ctx context.Context, actorID, invitationID string) error {
	return s.answer(ctx, actorID, invitationID, StatusDeclined)
}

func (s *Service) answer(ctx context.Context, actorID, invitationID, status string) error {
	return s.store.Answer(ctx, actorID, invitationID, status, s.now())
}

func isEmpty(value *string) bool { return value == nil || *value == "" }

// VenueHidden is true when the caller holds a SURPRISE invitation for the plan that has not
// been revealed yet. A member is never hidden from: they are already inside the plan.
func (s *PostgresStore) VenueHidden(ctx context.Context, userID, planID string) (bool, error) {
	const query = `
SELECT EXISTS (
    SELECT 1
    FROM core.tb_plan_invitation i
    JOIN core.tb_route r ON r.co_route = i.co_route
    WHERE i.co_route = $2::uuid
      AND i.co_user = $1::uuid
      AND i.tx_status = 'PENDING'
      AND i.tx_mode = 'SURPRISE'
      AND NOT (i.tx_reveal = 'AT_SCHEDULED' AND r.dt_scheduled_at IS NOT NULL
               AND now() >= r.dt_scheduled_at)
)`
	var hidden bool
	if err := s.pool.QueryRow(ctx, query, userID, planID).Scan(&hidden); err != nil {
		return false, fmt.Errorf("checking the reveal rule: %w", err)
	}
	return hidden, nil
}

// Expire flips stale PENDING rows to EXPIRED and reports how many it touched.
//
// This is hygiene, not enforcement: the read predicate already refuses to accept an expired
// invitation. If this job never runs, invitations are still correctly unusable — the table
// just keeps rows that claim to be pending.
func (s *PostgresStore) Expire(ctx context.Context, now time.Time) (int64, error) {
	const query = `
UPDATE core.tb_plan_invitation
   SET tx_status = 'EXPIRED', dt_responded_at = $1
 WHERE tx_status = 'PENDING' AND dt_expires_at <= $1`
	tag, err := s.pool.Exec(ctx, query, now)
	if err != nil {
		return 0, fmt.Errorf("expiring invitations: %w", err)
	}
	return tag.RowsAffected(), nil
}

// Sweeper is the scheduled hygiene job.
type Sweeper struct {
	store  Store
	logger *slog.Logger
	now    func() time.Time
}

// NewSweeper wires the job.
func NewSweeper(store Store, logger *slog.Logger) *Sweeper {
	if logger == nil {
		logger = slog.Default()
	}
	return &Sweeper{store: store, logger: logger, now: func() time.Time { return time.Now().UTC() }}
}

// Name satisfies the worker scheduler's job contract.
func (s *Sweeper) Name() string { return "invitation-expiry" }

// Run executes one sweep. A failure is logged by the scheduler and retried on the next tick.
func (s *Sweeper) Run(ctx context.Context) error {
	expired, err := s.store.Expire(ctx, s.now())
	if err != nil {
		return err
	}
	if expired > 0 {
		s.logger.Info("invitations expired", slog.Int64("count", expired))
	}
	return nil
}

// PostgresStore is the pgx implementation.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore wires the store.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

// Role returns the caller's role in the plan ("" when not a member) and its visibility.
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

// PlanVisible reports whether the plan exists at all; used to keep 404 and 403 apart.
func (s *PostgresStore) PlanVisible(ctx context.Context, actorID, planID string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM core.tb_route WHERE co_route = $1::uuid AND dt_deleted_at IS NULL)`,
		planID).Scan(&exists)
	return exists, err
}

const invitationColumns = `
SELECT i.co_invitation::text, i.co_route::text, i.co_user::text, i.co_inviter::text,
       i.tx_status::text, i.tx_mode::text, i.tx_message, i.tx_hint,
       i.dt_expires_at, i.dt_created_at, i.dt_responded_at,
       u.tx_name, r.tx_name, r.dt_scheduled_at,
       (i.tx_status = 'PENDING' AND i.dt_expires_at <= now()) AS expired,
       (i.tx_mode = 'PLAIN'
        OR (i.tx_reveal = 'ON_ACCEPT' AND i.tx_status = 'ACCEPTED')
        OR (i.tx_reveal = 'AT_SCHEDULED' AND r.dt_scheduled_at IS NOT NULL
            AND now() >= r.dt_scheduled_at)) AS revealed
FROM core.tb_plan_invitation i
JOIN seg.tb_user u ON u.co_user = i.co_user
JOIN core.tb_route r ON r.co_route = i.co_route`

// Create inserts a new invitation. A live one for the same person and plan is a conflict the
// partial unique index owns, not a pre-check that would race.
func (s *PostgresStore) Create(ctx context.Context, actorID, planID string, in CreateInput) (Invitation, error) {
	const query = `
INSERT INTO core.tb_plan_invitation
    (co_route, co_user, co_inviter, tx_status, tx_mode, tx_message, tx_hint,
     dt_expires_at, dt_created_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'PENDING', $4::public.plan_invitation_mode, $5, $6, $7, now())
RETURNING co_invitation::text`
	var invitationID string
	if err := s.pool.QueryRow(ctx, query, planID, in.UserID, actorID, in.Mode, in.Message, in.Hint,
		in.ExpiresAt).Scan(&invitationID); err != nil {
		if isUniqueViolation(err) {
			return Invitation{}, ErrAlreadyPending
		}
		return Invitation{}, fmt.Errorf("creating invitation: %w", err)
	}
	return s.byID(ctx, invitationID)
}

func (s *PostgresStore) byID(ctx context.Context, invitationID string) (Invitation, error) {
	row := s.pool.QueryRow(ctx, invitationColumns+` WHERE i.co_invitation = $1::uuid`, invitationID)
	invitation, err := scanInvitation(row)
	if err != nil {
		return Invitation{}, ErrNotFound
	}
	return invitation, nil
}

// ListForPlan is the host's view: every invitation of the plan, newest first.
func (s *PostgresStore) ListForPlan(ctx context.Context, planID string) ([]Invitation, error) {
	return s.list(ctx, invitationColumns+` WHERE i.co_route = $1::uuid ORDER BY i.dt_created_at DESC`, planID)
}

// ListForUser is the guest's view: only their own.
func (s *PostgresStore) ListForUser(ctx context.Context, userID string) ([]Invitation, error) {
	return s.list(ctx, invitationColumns+` WHERE i.co_user = $1::uuid ORDER BY i.dt_created_at DESC`, userID)
}

func (s *PostgresStore) list(ctx context.Context, query, arg string) ([]Invitation, error) {
	rows, err := s.pool.Query(ctx, query, arg)
	if err != nil {
		return nil, fmt.Errorf("listing invitations: %w", err)
	}
	defer rows.Close()

	result := make([]Invitation, 0, 8)
	for rows.Next() {
		invitation, err := scanInvitation(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning invitation: %w", err)
		}
		result = append(result, invitation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("during invitation iteration: %w", err)
	}
	return result, nil
}

// Answer flips a live invitation and, when accepting, creates the VIEWER membership.
//
// The WHERE clause carries the read predicate: a PENDING row past its expiry matches nothing, so
// an expired invitation cannot be accepted even if no sweeper ever ran (S2).
func (s *PostgresStore) Answer(ctx context.Context, actorID, invitationID, status string, at time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT set_config('app.current_user_id', $1, true)`, actorID); err != nil {
		return fmt.Errorf("announcing the actor: %w", err)
	}

	const flip = `
UPDATE core.tb_plan_invitation SET
    tx_status = $3::public.plan_invitation_status,
    dt_responded_at = $4
WHERE co_invitation = $1::uuid
  AND co_user = $2::uuid
  AND tx_status = 'PENDING'
  AND dt_expires_at > $4
RETURNING co_route::text`
	var planID string
	if err := tx.QueryRow(ctx, flip, invitationID, actorID, status, at).Scan(&planID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Not theirs, already answered, or expired: all indistinguishable on purpose.
			return ErrNotFound
		}
		return fmt.Errorf("answering invitation: %w", err)
	}

	if status == StatusAccepted {
		// Accepting grants the least privilege: VIEWER, never EDITOR (D8).
		const member = `
INSERT INTO core.tb_route_member (co_route, co_user, tx_role, dt_created_at, dt_updated_at)
VALUES ($1::uuid, $2::uuid, 'VIEWER', now(), now())
ON CONFLICT (co_route, co_user) DO NOTHING`
		if _, err := tx.Exec(ctx, member, planID, actorID); err != nil {
			return fmt.Errorf("creating the membership: %w", err)
		}
	}
	return tx.Commit(ctx)
}

// isUniqueViolation reports whether the error is a unique-constraint violation, which the
// partial index over PENDING invites surfaces as 23505.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func scanInvitation(row interface{ Scan(dest ...any) error }) (Invitation, error) {
	var invitation Invitation
	err := row.Scan(&invitation.ID, &invitation.PlanID, &invitation.UserID, &invitation.InviterID,
		&invitation.Status, &invitation.Mode, &invitation.Message, &invitation.Hint,
		&invitation.ExpiresAt, &invitation.CreatedAt, &invitation.RespondedAt,
		&invitation.InviteeName, &invitation.PlanName, &invitation.PlanAt,
		&invitation.Expired, &invitation.Revealed)
	return invitation, err
}
