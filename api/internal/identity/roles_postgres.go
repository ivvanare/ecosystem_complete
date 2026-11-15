package identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// withActor runs a write with the actor announced, so the history triggers on
// seg.tb_role_permission record WHO changed the composition: a capability change nobody can trace
// is indistinguishable from a compromise.
func (s *PostgresStore) withActor(ctx context.Context, actorID string, write func(pgx.Tx) error) error {
	tx, err := s.Pool.Begin(ctx)
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

// CreateRole inserts the role and its permission set in one transaction.
func (s *PostgresStore) CreateRole(ctx context.Context, actorID string, in RoleInput) (Role, error) {
	var roleID string
	err := s.withActor(ctx, actorID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO seg.tb_role (co_role, tx_name, tx_description, tx_status, dt_created_at, dt_updated_at)
			VALUES (gen_random_uuid(), $1, $2, 'ACTIVE'::public.activatable_status, now(), now())
			RETURNING co_role::text`, in.Name, in.Description).Scan(&roleID); err != nil {
			if uniqueViolation(err) {
				return ErrRoleNameTaken
			}
			return fmt.Errorf("inserting role: %w", err)
		}
		if err := ensurePermissions(ctx, tx, in.Permissions); err != nil {
			return err
		}
		return linkPermissions(ctx, tx, roleID, in.Permissions)
	})
	if err != nil {
		return Role{}, err
	}
	return s.Role(ctx, roleID)
}

// Role reads one live role with its permission codes.
func (s *PostgresStore) Role(ctx context.Context, roleID string) (Role, error) {
	const query = `
SELECT r.co_role::text, r.tx_name, r.tx_description, r.tx_status::text,
       COALESCE(array_agg(p.tx_code ORDER BY p.tx_code) FILTER (WHERE p.tx_code IS NOT NULL), '{}')
FROM seg.tb_role r
LEFT JOIN seg.tb_role_permission rp ON rp.co_role = r.co_role
LEFT JOIN seg.tb_permission p ON p.co_permission = rp.co_permission AND p.dt_deleted_at IS NULL
WHERE r.co_role = $1::uuid AND r.dt_deleted_at IS NULL
GROUP BY r.co_role, r.tx_name, r.tx_description, r.tx_status`
	var role Role
	if err := s.Pool.QueryRow(ctx, query, roleID).
		Scan(&role.ID, &role.Name, &role.Description, &role.Status, &role.Permissions); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Role{}, ErrRoleNotFound
		}
		return Role{}, fmt.Errorf("reading role: %w", err)
	}
	return role, nil
}

// UpdateRole applies a partial change. A nil pointer means "leave it as it is"; the description
// travels as a flag plus value so it can be cleared deliberately.
func (s *PostgresStore) UpdateRole(ctx context.Context, actorID, roleID string, in RoleUpdate) (Role, error) {
	var touched string
	err := s.withActor(ctx, actorID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			UPDATE seg.tb_role SET
			    tx_name        = COALESCE($2, tx_name),
			    tx_description = CASE WHEN $3::boolean THEN $4 ELSE tx_description END,
			    tx_status      = COALESCE($5::public.activatable_status, tx_status),
			    dt_updated_at  = now()
			 WHERE co_role = $1::uuid AND dt_deleted_at IS NULL
			RETURNING co_role::text`,
			roleID, in.Name, in.Description != nil, derefString(in.Description), in.Status).
			Scan(&touched)
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Role{}, ErrRoleNotFound
		}
		if uniqueViolation(err) {
			return Role{}, ErrRoleNameTaken
		}
		return Role{}, fmt.Errorf("updating role: %w", err)
	}
	return s.Role(ctx, roleID)
}

// RetireRole retires the role logically, refusing while anyone still holds it. The count and the
// retire share one transaction: the role row is locked FOR UPDATE, which also serializes against a
// concurrent assignment taking its KEY SHARE lock, so nobody can slip in between.
func (s *PostgresStore) RetireRole(ctx context.Context, actorID, roleID string) error {
	var touched string
	err := s.withActor(ctx, actorID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			SELECT co_role::text FROM seg.tb_role
			 WHERE co_role = $1::uuid AND dt_deleted_at IS NULL
			 FOR UPDATE`, roleID).Scan(&touched); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrRoleNotFound
			}
			return fmt.Errorf("locking the role: %w", err)
		}
		var users int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM seg.tb_user_role ur
			JOIN seg.tb_user u ON u.co_user = ur.co_user AND u.dt_deleted_at IS NULL
			WHERE ur.co_role = $1::uuid`, roleID).Scan(&users); err != nil {
			return fmt.Errorf("counting the users of a role: %w", err)
		}
		if users > 0 {
			return &RoleInUseError{Users: users}
		}
		if _, err := tx.Exec(ctx, `
			UPDATE seg.tb_role SET dt_deleted_at = now(), dt_updated_at = now()
			 WHERE co_role = $1::uuid`, roleID); err != nil {
			return fmt.Errorf("retiring the role: %w", err)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRoleNotFound
		}
		return fmt.Errorf("retiring role: %w", err)
	}
	return nil
}

// ReplaceRolePermissions replaces the whole composition in one transaction and writes only the
// difference: deleting every row and re-inserting the kept ones would record phantom DELETE+INSERT
// pairs in the history for permissions nobody touched.
func (s *PostgresStore) ReplaceRolePermissions(ctx context.Context, actorID, roleID string, codes []string) ([]string, error) {
	err := s.withActor(ctx, actorID, func(tx pgx.Tx) error {
		if err := ensurePermissions(ctx, tx, codes); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			DELETE FROM seg.tb_role_permission rp
			 WHERE rp.co_role = $1::uuid
			   AND rp.co_permission NOT IN (
			         SELECT p.co_permission FROM seg.tb_permission p
			          WHERE p.tx_code = ANY($2) AND p.tx_status = 'ACTIVE' AND p.dt_deleted_at IS NULL)`,
			roleID, codes); err != nil {
			return fmt.Errorf("removing permissions: %w", err)
		}
		return linkPermissions(ctx, tx, roleID, codes)
	})
	if err != nil {
		return nil, err
	}
	role, err := s.Role(ctx, roleID)
	if err != nil {
		return nil, err
	}
	return role.Permissions, nil
}

// RolesCarrying returns which of the named live roles grant the permission.
func (s *PostgresStore) RolesCarrying(ctx context.Context, permission string, roleNames []string) ([]string, error) {
	if len(roleNames) == 0 {
		return nil, nil
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT DISTINCT r.tx_name
		FROM seg.tb_role r
		JOIN seg.tb_role_permission rp ON rp.co_role = r.co_role
		JOIN seg.tb_permission p ON p.co_permission = rp.co_permission
		WHERE r.tx_name = ANY($1)
		  AND r.tx_status = 'ACTIVE' AND r.dt_deleted_at IS NULL
		  AND p.tx_code = $2 AND p.tx_status = 'ACTIVE' AND p.dt_deleted_at IS NULL`, roleNames, permission)
	if err != nil {
		return nil, fmt.Errorf("resolving the roles carrying a permission: %w", err)
	}
	defer rows.Close()

	carrying := make([]string, 0, 2)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scanning a role: %w", err)
		}
		carrying = append(carrying, name)
	}
	return carrying, rows.Err()
}

// RoleUserCount counts the users still holding the role, live accounts only.
func (s *PostgresStore) RoleUserCount(ctx context.Context, roleID string) (int, error) {
	var users int
	if err := s.Pool.QueryRow(ctx, `
		SELECT count(*) FROM seg.tb_user_role ur
		JOIN seg.tb_user u ON u.co_user = ur.co_user AND u.dt_deleted_at IS NULL
		WHERE ur.co_role = $1::uuid`, roleID).Scan(&users); err != nil {
		return 0, fmt.Errorf("counting the users of a role: %w", err)
	}
	return users, nil
}

// UnknownPermissions returns the codes the catalogue does not carry.
func (s *PostgresStore) UnknownPermissions(ctx context.Context, codes []string) ([]string, error) {
	if len(codes) == 0 {
		return nil, nil
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT code FROM unnest($1::text[]) AS code
		 WHERE NOT EXISTS (
		       SELECT 1 FROM seg.tb_permission p
		        WHERE p.tx_code = code AND p.tx_status = 'ACTIVE' AND p.dt_deleted_at IS NULL)`, codes)
	if err != nil {
		return nil, fmt.Errorf("checking permissions: %w", err)
	}
	defer rows.Close()

	unknown := make([]string, 0, 2)
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, fmt.Errorf("scanning unknown permission: %w", err)
		}
		unknown = append(unknown, code)
	}
	return unknown, rows.Err()
}

// ensurePermissions refuses codes the catalogue does not carry, inside the same transaction as the
// write: the service validates first for a clean 422, but a permission retired between that check
// and this insert would otherwise be dropped without a word.
func ensurePermissions(ctx context.Context, tx pgx.Tx, codes []string) error {
	if len(codes) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `
		SELECT tx_code FROM seg.tb_permission
		 WHERE tx_code = ANY($1) AND tx_status = 'ACTIVE' AND dt_deleted_at IS NULL`, codes)
	if err != nil {
		return fmt.Errorf("resolving permissions: %w", err)
	}
	resolved := make(map[string]struct{}, len(codes))
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			rows.Close()
			return fmt.Errorf("scanning permission: %w", err)
		}
		resolved[code] = struct{}{}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("resolving permissions: %w", err)
	}
	missing := make([]string, 0, 2)
	for _, code := range codes {
		if _, ok := resolved[code]; !ok {
			missing = append(missing, code)
		}
	}
	if len(missing) > 0 {
		return &UnknownPermissionError{Codes: missing}
	}
	return nil
}

// linkPermissions resolves active permission codes to ids and links them, leaving an existing link
// alone so the history records only what actually changed.
func linkPermissions(ctx context.Context, tx pgx.Tx, roleID string, codes []string) error {
	if len(codes) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO seg.tb_role_permission (co_role, co_permission, dt_created_at, dt_updated_at)
		SELECT $1::uuid, p.co_permission, now(), now()
		FROM seg.tb_permission p
		WHERE p.tx_code = ANY($2) AND p.tx_status = 'ACTIVE' AND p.dt_deleted_at IS NULL
		ON CONFLICT DO NOTHING`, roleID, codes); err != nil {
		return fmt.Errorf("linking permissions: %w", err)
	}
	return nil
}

// uniqueViolation reports whether the database refused a duplicate key.
func uniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// derefString reads an optional description: nil and empty pointer both mean "no description".
func derefString(value **string) *string {
	if value == nil || *value == nil {
		return nil
	}
	return *value
}
