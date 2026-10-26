package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/audit"
)

// ErrUnknownRole means at least one requested role name does not exist.
var ErrUnknownRole = errors.New("identity: unknown role")

// Page is one page of results.
type Page[T any] struct {
	Data  []T
	Page  int
	Limit int
	Total int
}

// UserSummary is one row of the user list.
type UserSummary struct {
	ID              string
	Name            *string
	Email           *string
	Status          string
	EmailVerifiedAt *time.Time
	CreatedAt       time.Time
	Roles           []string
}

// UserFilter narrows the user list.
type UserFilter struct {
	Page   int
	Limit  int
	Query  string
	Status string
}

// AuditEvent is one row of the security log.
type AuditEvent struct {
	ID        string
	Event     string
	UserID    *string
	ActorID   *string
	EmailHash *string
	IP        *string
	Country   *string
	City      *string
	UserAgent *string
	Metadata  map[string]any
	CreatedAt time.Time
}

// AuditFilter narrows the audit list.
type AuditFilter struct {
	Page   int
	Limit  int
	Event  string
	UserID string
	From   *time.Time
	To     *time.Time
}

// AdminStore is the administration surface.
type AdminStore interface {
	ListUsers(ctx context.Context, filter UserFilter) (Page[UserSummary], error)
	SetRoles(ctx context.Context, userID string, roles []string) ([]string, error)
	ListAuditEvents(ctx context.Context, filter AuditFilter) (Page[AuditEvent], error)
}

// SetRoles replaces the roles of a user and audits the change.
//
// The audit row names the actor (the administrator) and the subject (the user), because a
// capability change that nobody can trace is indistinguishable from a compromise.
func (s *Service) SetRoles(ctx context.Context, actorID, userID string, roles []string, meta audit.Event) ([]string, error) {
	effective, err := s.Admin.SetRoles(ctx, userID, roles)
	if err != nil {
		return nil, err
	}

	if s.Auditor != nil {
		event := meta
		event.Name = audit.EventRolesChanged
		event.UserID = userID
		event.ActorID = actorID
		event.Metadata = map[string]any{"roles": effective}
		if err := s.Auditor.Record(ctx, event); err != nil && s.Logger != nil {
			s.Logger.Warn("audit write failed", "event", event.Name, "error", err.Error())
		}
	}

	return effective, nil
}

// Users returns one page of users.
func (s *Service) Users(ctx context.Context, filter UserFilter) (Page[UserSummary], error) {
	return s.Admin.ListUsers(ctx, filter)
}

// AuditEvents returns one page of the security log.
func (s *Service) AuditEvents(ctx context.Context, filter AuditFilter) (Page[AuditEvent], error) {
	return s.Admin.ListAuditEvents(ctx, filter)
}

// normalizePaging applies the contract's defaults: page 1, limit 20, maximum 100.
func normalizePaging(page, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return page, limit
}

// offset is the SQL OFFSET for a page.
func offset(page, limit int) int { return (page - 1) * limit }

func validateRoles(roles []string) error {
	seen := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		if role == "" {
			return fmt.Errorf("%w: empty role name", ErrUnknownRole)
		}
		if _, dup := seen[role]; dup {
			return fmt.Errorf("%w: duplicated %q", ErrUnknownRole, role)
		}
		seen[role] = struct{}{}
	}
	return nil
}
