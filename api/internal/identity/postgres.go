package identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore reads users, roles and permissions.
type PostgresStore struct {
	Pool *pgxpool.Pool
}

// NewPostgresStore returns a Store backed by the given pool.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{Pool: pool}
}

// Profile reads one active user.
func (s *PostgresStore) Profile(ctx context.Context, userID string) (Profile, error) {
	var profile Profile
	err := s.Pool.QueryRow(ctx, `
		SELECT co_user::text, tx_name, tx_email, tx_status::text, dt_email_verified_at, dt_created_at
		FROM seg.tb_user
		WHERE co_user = $1::uuid AND dt_deleted_at IS NULL`, userID,
	).Scan(&profile.ID, &profile.Name, &profile.Email, &profile.Status,
		&profile.EmailVerifiedAt, &profile.CreatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, ErrNotFound
	}
	if err != nil {
		return Profile{}, fmt.Errorf("selecting tb_user: %w", err)
	}
	return profile, nil
}

// Access resolves the live roles and permissions.
//
// Only active roles and active permissions count: disabling a role or a permission takes
// effect on the next request, which is the whole reason capability is not in the token.
func (s *PostgresStore) Access(ctx context.Context, userID string) (Access, error) {
	access := Access{Roles: []string{}, Permissions: []string{}}

	rows, err := s.Pool.Query(ctx, `
		SELECT r.tx_name
		FROM seg.tb_user_role ur
		JOIN seg.tb_role r ON r.co_role = ur.co_role
		WHERE ur.co_user = $1::uuid
		  AND r.tx_status = 'ACTIVE' AND r.dt_deleted_at IS NULL
		ORDER BY r.tx_name`, userID)
	if err != nil {
		return Access{}, fmt.Errorf("selecting roles: %w", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return Access{}, fmt.Errorf("scanning role: %w", err)
		}
		access.Roles = append(access.Roles, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Access{}, fmt.Errorf("reading roles: %w", err)
	}

	rows, err = s.Pool.Query(ctx, `
		SELECT DISTINCT p.tx_code
		FROM seg.tb_user_role ur
		JOIN seg.tb_role r            ON r.co_role = ur.co_role
		JOIN seg.tb_role_permission rp ON rp.co_role = r.co_role
		JOIN seg.tb_permission p       ON p.co_permission = rp.co_permission
		WHERE ur.co_user = $1::uuid
		  AND r.tx_status = 'ACTIVE' AND r.dt_deleted_at IS NULL
		  AND p.tx_status = 'ACTIVE' AND p.dt_deleted_at IS NULL
		ORDER BY p.tx_code`, userID)
	if err != nil {
		return Access{}, fmt.Errorf("selecting permissions: %w", err)
	}
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			rows.Close()
			return Access{}, fmt.Errorf("scanning permission: %w", err)
		}
		access.Permissions = append(access.Permissions, code)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Access{}, fmt.Errorf("reading permissions: %w", err)
	}

	return access, nil
}

// Role is a catalogue entry with its permission codes.
type Role struct {
	ID          string
	Name        string
	Description *string
	Status      string
	Permissions []string
}

// ListRoles returns every live role.
func (s *PostgresStore) ListRoles(ctx context.Context) ([]Role, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT r.co_role::text, r.tx_name, r.tx_description, r.tx_status::text,
		       COALESCE(array_agg(p.tx_code ORDER BY p.tx_code)
		                FILTER (WHERE p.tx_code IS NOT NULL), '{}')
		FROM seg.tb_role r
		LEFT JOIN seg.tb_role_permission rp ON rp.co_role = r.co_role
		LEFT JOIN seg.tb_permission p       ON p.co_permission = rp.co_permission
		                                   AND p.dt_deleted_at IS NULL
		WHERE r.dt_deleted_at IS NULL
		GROUP BY r.co_role, r.tx_name, r.tx_description, r.tx_status
		ORDER BY r.tx_name`)
	if err != nil {
		return nil, fmt.Errorf("selecting roles: %w", err)
	}
	defer rows.Close()

	roles := make([]Role, 0, 8)
	for rows.Next() {
		var role Role
		if err := rows.Scan(&role.ID, &role.Name, &role.Description, &role.Status, &role.Permissions); err != nil {
			return nil, fmt.Errorf("scanning role: %w", err)
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

// Permission is a catalogue entry.
type Permission struct {
	ID     string
	Code   string
	Module string
	Name   string
	Status string
}

// ListPermissions returns every live permission, grouped for the backoffice UI by module.
func (s *PostgresStore) ListPermissions(ctx context.Context) ([]Permission, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT co_permission::text, tx_code, tx_module, tx_name, tx_status::text
		FROM seg.tb_permission
		WHERE dt_deleted_at IS NULL
		ORDER BY tx_module, tx_code`)
	if err != nil {
		return nil, fmt.Errorf("selecting permissions: %w", err)
	}
	defer rows.Close()

	permissions := make([]Permission, 0, 16)
	for rows.Next() {
		var permission Permission
		if err := rows.Scan(&permission.ID, &permission.Code, &permission.Module,
			&permission.Name, &permission.Status); err != nil {
			return nil, fmt.Errorf("scanning permission: %w", err)
		}
		permissions = append(permissions, permission)
	}
	return permissions, rows.Err()
}
