package identity

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

// ListUsers returns one page of active users with their roles.
func (s *PostgresStore) ListUsers(ctx context.Context, filter UserFilter) (Page[UserSummary], error) {
	page, limit := normalizePaging(filter.Page, filter.Limit)

	const where = `
		u.dt_deleted_at IS NULL
		AND ($1 = '' OR u.tx_email ILIKE '%' || $1 || '%' OR COALESCE(u.tx_name, '') ILIKE '%' || $1 || '%')
		AND ($2 = '' OR u.tx_status::text = $2)`

	var total int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM seg.tb_user u WHERE `+where,
		filter.Query, filter.Status).Scan(&total); err != nil {
		return Page[UserSummary]{}, fmt.Errorf("counting users: %w", err)
	}

	rows, err := s.Pool.Query(ctx, `
		SELECT u.co_user::text, u.tx_name, u.tx_email, u.tx_status::text,
		       u.dt_email_verified_at, u.dt_created_at,
		       COALESCE(array_agg(r.tx_name ORDER BY r.tx_name)
		                FILTER (WHERE r.tx_name IS NOT NULL), '{}')
		FROM seg.tb_user u
		LEFT JOIN seg.tb_user_role ur ON ur.co_user = u.co_user
		LEFT JOIN seg.tb_role r ON r.co_role = ur.co_role
		                        AND r.tx_status = 'ACTIVE' AND r.dt_deleted_at IS NULL
		WHERE `+where+`
		GROUP BY u.co_user, u.tx_name, u.tx_email, u.tx_status, u.dt_email_verified_at, u.dt_created_at
		ORDER BY u.dt_created_at DESC
		LIMIT $3 OFFSET $4`,
		filter.Query, filter.Status, limit, offset(page, limit))
	if err != nil {
		return Page[UserSummary]{}, fmt.Errorf("selecting users: %w", err)
	}
	defer rows.Close()

	data := make([]UserSummary, 0, limit)
	for rows.Next() {
		var user UserSummary
		if err := rows.Scan(&user.ID, &user.Name, &user.Email, &user.Status,
			&user.EmailVerifiedAt, &user.CreatedAt, &user.Roles); err != nil {
			return Page[UserSummary]{}, fmt.Errorf("scanning user: %w", err)
		}
		data = append(data, user)
	}
	if err := rows.Err(); err != nil {
		return Page[UserSummary]{}, fmt.Errorf("reading users: %w", err)
	}

	return Page[UserSummary]{Data: data, Page: page, Limit: limit, Total: total}, nil
}

// SetRoles replaces the complete role set of a user and returns the effective names.
//
// Replace, not merge: the bridge table is rewritten in one transaction, so a role removed in
// the request is really gone. Unknown or inactive role names abort the whole thing rather
// than half-applying a capability change.
func (s *PostgresStore) SetRoles(ctx context.Context, userID string, roles []string) ([]string, error) {
	if err := validateRoles(roles); err != nil {
		return nil, err
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	settled := false
	defer func() {
		if !settled {
			_ = tx.Rollback(ctx)
		}
	}()

	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM seg.tb_user WHERE co_user = $1::uuid AND dt_deleted_at IS NULL)`,
		userID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("checking user: %w", err)
	}
	if !exists {
		return nil, ErrNotFound
	}

	rows, err := tx.Query(ctx, `
		SELECT co_role::text, tx_name
		FROM seg.tb_role
		WHERE tx_name = ANY($1) AND tx_status = 'ACTIVE' AND dt_deleted_at IS NULL`, roles)
	if err != nil {
		return nil, fmt.Errorf("resolving roles: %w", err)
	}

	ids := make([]string, 0, len(roles))
	names := make([]string, 0, len(roles))
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning role: %w", err)
		}
		ids = append(ids, id)
		names = append(names, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading roles: %w", err)
	}
	if len(ids) != len(roles) {
		return nil, fmt.Errorf("%w: one or more names do not exist or are inactive", ErrUnknownRole)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM seg.tb_user_role WHERE co_user = $1::uuid`, userID); err != nil {
		return nil, fmt.Errorf("clearing roles: %w", err)
	}

	if len(ids) > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO seg.tb_user_role (co_user, co_role, dt_created_at, dt_updated_at)
			SELECT $1::uuid, unnest($2::uuid[]), now(), now()`, userID, ids); err != nil {
			return nil, fmt.Errorf("assigning roles: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing roles: %w", err)
	}
	settled = true

	sort.Strings(names)
	return names, nil
}

// ListAuditEvents returns one page of the security log, newest first.
func (s *PostgresStore) ListAuditEvents(ctx context.Context, filter AuditFilter) (Page[AuditEvent], error) {
	page, limit := normalizePaging(filter.Page, filter.Limit)

	const where = `
		($1 = '' OR tx_event = $1)
		AND ($2 = '' OR co_user = $2::uuid)
		AND ($3::timestamptz IS NULL OR dt_created_at >= $3)
		AND ($4::timestamptz IS NULL OR dt_created_at <= $4)`

	var total int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM seg.tb_security_event WHERE `+where,
		filter.Event, filter.UserID, filter.From, filter.To).Scan(&total); err != nil {
		return Page[AuditEvent]{}, fmt.Errorf("counting events: %w", err)
	}

	rows, err := s.Pool.Query(ctx, `
		SELECT co_security_event::text, tx_event, co_user::text, co_actor_user::text,
		       tx_email_hash, tx_ip, tx_country, tx_city, tx_user_agent, js_metadata, dt_created_at
		FROM seg.tb_security_event
		WHERE `+where+`
		ORDER BY dt_created_at DESC
		LIMIT $5 OFFSET $6`,
		filter.Event, filter.UserID, filter.From, filter.To, limit, offset(page, limit))
	if err != nil {
		return Page[AuditEvent]{}, fmt.Errorf("selecting events: %w", err)
	}
	defer rows.Close()

	data := make([]AuditEvent, 0, limit)
	for rows.Next() {
		var event AuditEvent
		var metadata []byte
		if err := rows.Scan(&event.ID, &event.Event, &event.UserID, &event.ActorID,
			&event.EmailHash, &event.IP, &event.Country, &event.City,
			&event.UserAgent, &metadata, &event.CreatedAt); err != nil {
			return Page[AuditEvent]{}, fmt.Errorf("scanning event: %w", err)
		}
		if len(metadata) > 0 {
			if err := json.Unmarshal(metadata, &event.Metadata); err != nil {
				return Page[AuditEvent]{}, fmt.Errorf("decoding event metadata: %w", err)
			}
		}
		data = append(data, event)
	}
	if err := rows.Err(); err != nil {
		return Page[AuditEvent]{}, fmt.Errorf("reading events: %w", err)
	}

	return Page[AuditEvent]{Data: data, Page: page, Limit: limit, Total: total}, nil
}
