// Package identity answers "who is this user and what may they do".
//
// Capability is derived from live RBAC (user → role → permission) on every call, never from
// the access token. A token that embedded permissions would keep a revoked role alive until
// it expired.
package identity

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound means the user does not exist or was deleted.
var ErrNotFound = errors.New("identity: user not found")

// Profile is the user row.
type Profile struct {
	ID              string
	Name            *string
	Email           *string
	Status          string
	EmailVerifiedAt *time.Time
	CreatedAt       time.Time
}

// Access is the effective capability.
type Access struct {
	Roles       []string
	Permissions []string
}

// Me is the profile plus its capability.
type Me struct {
	Profile
	Access
}

// Store reads identity and capability.
type Store interface {
	Profile(ctx context.Context, userID string) (Profile, error)
	Access(ctx context.Context, userID string) (Access, error)
}

// CatalogueStore reads the role and permission catalogues for the backoffice.
type CatalogueStore interface {
	ListRoles(ctx context.Context) ([]Role, error)
	ListPermissions(ctx context.Context) ([]Permission, error)
}

// Service serves the identity queries.
type Service struct {
	Store     Store
	Catalogue CatalogueStore
}

// New returns a Service.
func New(store Store, catalogue CatalogueStore) *Service {
	return &Service{Store: store, Catalogue: catalogue}
}

// Roles returns the role catalogue.
func (s *Service) Roles(ctx context.Context) ([]Role, error) {
	return s.Catalogue.ListRoles(ctx)
}

// Permissions returns the permission catalogue.
func (s *Service) Permissions(ctx context.Context) ([]Permission, error) {
	return s.Catalogue.ListPermissions(ctx)
}

// Me returns the profile with its effective roles and permissions.
func (s *Service) Me(ctx context.Context, userID string) (Me, error) {
	profile, err := s.Store.Profile(ctx, userID)
	if err != nil {
		return Me{}, err
	}
	access, err := s.Store.Access(ctx, userID)
	if err != nil {
		return Me{}, fmt.Errorf("identity: reading access: %w", err)
	}
	return Me{Profile: profile, Access: access}, nil
}

// Can reports whether the user holds the permission right now.
func (s *Service) Can(ctx context.Context, userID, permission string) (bool, error) {
	access, err := s.Store.Access(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("identity: reading access: %w", err)
	}
	for _, code := range access.Permissions {
		if code == permission {
			return true, nil
		}
	}
	return false, nil
}
