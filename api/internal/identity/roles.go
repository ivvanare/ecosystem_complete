package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// SystemRole is the role the system protects from itself: it cannot be renamed, retired, or left
// without role.manage. Whoever edits roles could otherwise remove the last way back in.
const SystemRole = "SUPERADMIN"

// ManagedPermission is what authorizes role authoring, and the one permission the system role may
// never lose.
const ManagedPermission = "role.manage"

// Errors the transport maps onto responses.
var (
	// ErrRoleNotFound covers missing and retired alike: a retired role is not a role anyone edits.
	ErrRoleNotFound = errors.New("identity: role not found")
	// ErrRoleProtected is the system role refusing a change that could lock the system out.
	ErrRoleProtected = errors.New("identity: the system role is protected")
	// ErrRoleNameTaken is a duplicate among live roles.
	ErrRoleNameTaken = errors.New("identity: a live role already has that name")
	// ErrInvalidRoleName, ErrInvalidRoleStatus and ErrInvalidRolePermissions are the edge
	// validations of a role write. Separate sentinels because the transport answers with the field
	// that failed, and one shared error would have to guess it.
	ErrInvalidRoleName        = errors.New("identity: invalid role name")
	ErrInvalidRoleStatus      = errors.New("identity: invalid role status")
	ErrInvalidRolePermissions = errors.New("identity: invalid permission set")
)

// RoleInUseError carries how many users still hold the role, so the answer can say it.
type RoleInUseError struct{ Users int }

func (e *RoleInUseError) Error() string {
	return fmt.Sprintf("identity: %d users still hold the role", e.Users)
}

// UnknownPermissionError names the codes the catalogue does not carry.
type UnknownPermissionError struct{ Codes []string }

func (e *UnknownPermissionError) Error() string {
	return fmt.Sprintf("identity: unknown permissions %v", e.Codes)
}

// RoleInput is a role as an operator writes it.
type RoleInput struct {
	Name        string
	Description *string
	Permissions []string
}

// RoleUpdate is a partial edit: a nil field means "leave it as it is". Description is a pointer to
// a pointer so it can be cleared deliberately.
type RoleUpdate struct {
	Name        *string
	Description **string
	Status      *string
}

// RoleStore is the persistence seam of role authoring. It lives apart from AdminStore so the
// read-only administration surface keeps its narrow interface.
type RoleStore interface {
	CreateRole(ctx context.Context, actorID string, in RoleInput) (Role, error)
	Role(ctx context.Context, roleID string) (Role, error)
	UpdateRole(ctx context.Context, actorID, roleID string, in RoleUpdate) (Role, error)
	RetireRole(ctx context.Context, actorID, roleID string) error
	ReplaceRolePermissions(ctx context.Context, actorID, roleID string, codes []string) ([]string, error)
	RoleUserCount(ctx context.Context, roleID string) (int, error)
	UnknownPermissions(ctx context.Context, codes []string) ([]string, error)
}

// CreateRole validates the shapes and writes the role with its permission set.
func (s *Service) CreateRole(ctx context.Context, actorID string, in RoleInput) (Role, error) {
	name, err := validateRoleName(in.Name)
	if err != nil {
		return Role{}, err
	}
	codes, err := validatePermissionCodes(in.Permissions)
	if err != nil {
		return Role{}, err
	}
	if err := s.requirePermissions(ctx, codes); err != nil {
		return Role{}, err
	}
	return s.RoleAuthor.CreateRole(ctx, actorID, RoleInput{Name: name, Description: in.Description, Permissions: codes})
}

// Role returns one live role with its permission codes.
func (s *Service) Role(ctx context.Context, roleID string) (Role, error) {
	return s.RoleAuthor.Role(ctx, roleID)
}

// UpdateRole edits the fields an operator owns, refusing the change that could lock the system out.
func (s *Service) UpdateRole(ctx context.Context, actorID, roleID string, in RoleUpdate) (Role, error) {
	current, err := s.RoleAuthor.Role(ctx, roleID)
	if err != nil {
		return Role{}, err
	}
	if in.Name != nil {
		name, err := validateRoleName(*in.Name)
		if err != nil {
			return Role{}, err
		}
		if current.Name == SystemRole && name != SystemRole {
			return Role{}, ErrRoleProtected
		}
		in.Name = &name
	}
	if in.Status != nil {
		if err := validateRoleStatus(*in.Status); err != nil {
			return Role{}, err
		}
		// Deactivating the system role would revoke every capability it grants, including the one
		// that manages roles. It is the same lockout as renaming it, one step further.
		if current.Name == SystemRole && *in.Status != "ACTIVE" {
			return Role{}, ErrRoleProtected
		}
	}
	return s.RoleAuthor.UpdateRole(ctx, actorID, roleID, in)
}

// RetireRole deletes a role logically, refusing to do it over the heads of the users who hold it.
func (s *Service) RetireRole(ctx context.Context, actorID, roleID string) error {
	current, err := s.RoleAuthor.Role(ctx, roleID)
	if err != nil {
		return err
	}
	if current.Name == SystemRole {
		return ErrRoleProtected
	}
	users, err := s.RoleAuthor.RoleUserCount(ctx, roleID)
	if err != nil {
		return err
	}
	if users > 0 {
		return &RoleInUseError{Users: users}
	}
	return s.RoleAuthor.RetireRole(ctx, actorID, roleID)
}

// ReplaceRolePermissions replaces the composition of a role as a whole set.
func (s *Service) ReplaceRolePermissions(ctx context.Context, actorID, roleID string, codes []string) ([]string, error) {
	current, err := s.RoleAuthor.Role(ctx, roleID)
	if err != nil {
		return nil, err
	}
	cleaned, err := validatePermissionCodes(codes)
	if err != nil {
		return nil, err
	}
	if err := s.requirePermissions(ctx, cleaned); err != nil {
		return nil, err
	}
	// The system role must keep the capability that manages roles: a set without it is the same
	// lockout as deleting the role, arriving through the other door.
	if current.Name == SystemRole {
		kept := false
		for _, code := range cleaned {
			if code == ManagedPermission {
				kept = true
				break
			}
		}
		if !kept {
			return nil, ErrRoleProtected
		}
	}
	return s.RoleAuthor.ReplaceRolePermissions(ctx, actorID, roleID, cleaned)
}

// requirePermissions refuses codes the catalogue does not carry: a silently dropped permission is
// a role that does less than the operator believes.
func (s *Service) requirePermissions(ctx context.Context, codes []string) error {
	if len(codes) == 0 {
		return nil
	}
	unknown, err := s.RoleAuthor.UnknownPermissions(ctx, codes)
	if err != nil {
		return err
	}
	if len(unknown) > 0 {
		return &UnknownPermissionError{Codes: unknown}
	}
	return nil
}

// validateRoleName enforces the shape the contract declares.
func validateRoleName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", fmt.Errorf("%w: the name is required", ErrInvalidRoleName)
	}
	if len(trimmed) > 60 {
		return "", fmt.Errorf("%w: the name is longer than 60 characters", ErrInvalidRoleName)
	}
	return trimmed, nil
}

// validateRoleStatus accepts the two states a role may be in.
func validateRoleStatus(status string) error {
	if status != "ACTIVE" && status != "INACTIVE" {
		return fmt.Errorf("%w: unknown status %q", ErrInvalidRoleStatus, status)
	}
	return nil
}

// validatePermissionCodes trims, refuses blanks and duplicates: two rows for the same permission
// is a composition nobody meant.
func validatePermissionCodes(codes []string) ([]string, error) {
	seen := make(map[string]struct{}, len(codes))
	cleaned := make([]string, 0, len(codes))
	for _, code := range codes {
		trimmed := strings.TrimSpace(code)
		if trimmed == "" {
			return nil, fmt.Errorf("%w: an empty permission code", ErrInvalidRolePermissions)
		}
		if _, dup := seen[trimmed]; dup {
			return nil, fmt.Errorf("%w: duplicated permission %q", ErrInvalidRolePermissions, trimmed)
		}
		seen[trimmed] = struct{}{}
		cleaned = append(cleaned, trimmed)
	}
	return cleaned, nil
}
