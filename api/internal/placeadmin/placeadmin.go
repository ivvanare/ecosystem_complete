// Package placeadmin is the places backoffice: the routes an operator uses to curate the
// catalogue, with an ownership viewer that decides who may see and change what (D21).
package placeadmin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Errors the transport maps onto responses.
var (
	// ErrNotFound covers invisible, soft-deleted and missing alike: a caller who may not
	// see a row must not learn that it exists, so "not yours" and "not there" answer the
	// same way.
	ErrNotFound = errors.New("placeadmin: place not found")
	// ErrUnknownCategory is a validated failure: the category must exist and be live.
	ErrUnknownCategory = errors.New("placeadmin: unknown category")
)

// Viewer is who asks. ManageAll is true for a caller holding admin.access, resolved once at
// the transport from the Authorizer — never from the body and never from a token claim.
type Viewer struct {
	UserID    string
	ManageAll bool
}

// Place is one row as the backoffice reads it.
type Place struct {
	ID           string
	OwnerUserID  *string
	Name         string
	Title        string
	Description  *string
	CategoryID   string
	CategoryName string
	Country      string
	Region       *string
	Timezone     string
	Latitude     float64
	Longitude    float64
	Address      *string
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    *time.Time
}

// Filter narrows the listing. Every field is optional; the zero value lists every row the
// viewer may see.
type Filter struct {
	Query      string
	Status     string
	CategoryID string
	Country    string
	Region     string
}

// CreateInput is a new place. The owner is assigned by backoffice only.
type CreateInput struct {
	OwnerUserID *string
	Name        string
	Title       string
	Description *string
	CategoryID  string
	Country     string
	Region      *string
	Timezone    string
	Latitude    float64
	Longitude   float64
	Address     *string
}

// UpdateInput is a partial update: a nil field means "leave it as it is". Address and
// description are pointers to pointers so they can be cleared deliberately.
type UpdateInput struct {
	Name        *string
	Title       *string
	Description **string
	CategoryID  *string
	Country     *string
	Region      **string
	Timezone    *string
	Latitude    *float64
	Longitude   *float64
	Address     **string
	OwnerUserID **string
	Status      *string
}

// Category is one entry of the category catalogue. The taxonomy is public: a client needs it
// to render a filter, and an operator needs it to render a select. It is not customer data.
type Category struct {
	ID     string
	Code   string
	Name   string
	Status string
}

// Page is one page of the listing.
type Page struct {
	Rows  []Place
	Total int
	Page  int
	Limit int
}

// Store is the persistence seam; tests fake it, because the suite never needs PostgreSQL.
type Store interface {
	List(ctx context.Context, viewer Viewer, filter Filter, page, limit int) ([]Place, int, error)
	Get(ctx context.Context, viewer Viewer, placeID string) (Place, error)
	Create(ctx context.Context, actorID string, in CreateInput) (Place, error)
	Update(ctx context.Context, viewer Viewer, actorID, placeID string, in UpdateInput) (Place, error)
	Retire(ctx context.Context, viewer Viewer, actorID, placeID string) error
	CategoryExists(ctx context.Context, categoryID string) (bool, error)
	Categories(ctx context.Context, status string, page, limit int) ([]Category, int, error)
}

// Service applies the ownership rules.
type Service struct {
	store  Store
	logger *slog.Logger
}

// NewService wires the backoffice.
func NewService(store Store, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{store: store, logger: logger}
}

// List returns the places the viewer may see (S2).
func (s *Service) List(ctx context.Context, viewer Viewer, filter Filter, page, limit int) (Page, error) {
	rows, total, err := s.store.List(ctx, viewer, filter, page, limit)
	if err != nil {
		return Page{}, err
	}
	return Page{Rows: rows, Total: total, Page: page, Limit: limit}, nil
}

// Get returns one place, scoped by ownership.
func (s *Service) Get(ctx context.Context, viewer Viewer, placeID string) (Place, error) {
	return s.store.Get(ctx, viewer, placeID)
}

// Create inserts a place. The transport has already required admin.access (S3); the service
// only checks that the named category exists, because a silent FK violation is a 500 the
// caller cannot understand.
func (s *Service) Create(ctx context.Context, actorID string, in CreateInput) (Place, error) {
	if err := s.requireCategory(ctx, in.CategoryID); err != nil {
		return Place{}, err
	}
	return s.store.Create(ctx, actorID, in)
}

// Update changes one place, scoped by ownership: a row the viewer cannot see is ErrNotFound,
// not a permission error (S4).
func (s *Service) Update(ctx context.Context, viewer Viewer, actorID, placeID string, in UpdateInput) (Place, error) {
	if in.CategoryID != nil {
		if err := s.requireCategory(ctx, *in.CategoryID); err != nil {
			return Place{}, err
		}
	}
	return s.store.Update(ctx, viewer, actorID, placeID, in)
}

// Retire logically deletes a place (S5).
func (s *Service) Retire(ctx context.Context, viewer Viewer, actorID, placeID string) error {
	return s.store.Retire(ctx, viewer, actorID, placeID)
}

func (s *Service) requireCategory(ctx context.Context, categoryID string) error {
	exists, err := s.store.CategoryExists(ctx, categoryID)
	if err != nil {
		return fmt.Errorf("checking category: %w", err)
	}
	if !exists {
		return ErrUnknownCategory
	}
	return nil
}

// Categories lists the catalogue. An empty status means "every live category", so a client
// that wants only the selectable ones passes ACTIVE and a client browsing passes nothing.
func (s *Service) Categories(ctx context.Context, status string, page, limit int) ([]Category, int, error) {
	return s.store.Categories(ctx, status, page, limit)
}

// PostgresStore is the pgx implementation.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore wires the store.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

// scopePredicate is the ownership rule in SQL: an admin sees everything, everybody else sees
// only rows they own. A curated row (NULL owner) therefore has no owner-visibility at all,
// which is what the schema comment always meant.
const scopePredicate = `($1::boolean OR p.co_owner_user = $2::uuid)`

const placeColumns = `
SELECT p.co_place::text, p.co_owner_user::text, p.tx_name, p.tx_title, p.tx_description,
       p.co_place_category::text, c.tx_name,
       p.tx_country, p.tx_region, p.tx_timezone,
       p.nu_latitude::float8, p.nu_longitude::float8, p.tx_address,
       p.tx_status::text, p.dt_created_at, p.dt_updated_at, p.dt_deleted_at
FROM core.tb_place p
JOIN core.tb_place_category c ON c.co_place_category = p.co_place_category`

// List returns one page scoped to the viewer and the total under the same predicate.
func (s *PostgresStore) List(ctx context.Context, viewer Viewer, filter Filter, page, limit int) ([]Place, int, error) {
	// Two honest variants: the live catalogue (the default, which is what an operator
	// manages) or the retired rows when they are asked for by name.
	base := `
WHERE ` + scopePredicate + `
  AND ($4::uuid IS NULL OR p.co_place_category = $4::uuid)
  AND ($5::char(2) IS NULL OR p.tx_country = $5::char(2))
  AND ($6::varchar IS NULL OR p.tx_region = $6::varchar)
  AND ($7::text IS NULL OR p.tx_title ILIKE '%' || $7 || '%')`

	predicate := base + `
  AND p.dt_deleted_at IS NULL
  AND ($3::public.suspendable_status IS NULL OR p.tx_status = $3::public.suspendable_status)`
	if filter.Status == "DELETED" {
		// $3 stays referenced even here: an unused placeholder leaves Postgres unable to
		// infer its type ("could not determine data type of parameter $3"). A retired row
		// still carries a status, so narrowing by it is also the useful behaviour.
		predicate = base + `
  AND p.dt_deleted_at IS NOT NULL
  AND ($3::public.suspendable_status IS NULL OR p.tx_status = $3::public.suspendable_status)`
	}

	// DELETED is a listing selector, not a value of the status enum: it chooses the retired
	// rows and must not be bound to a suspendable_status parameter.
	statusParam := filter.Status
	if statusParam == "DELETED" {
		statusParam = ""
	}
	args := []any{viewer.ManageAll, uuidOrNil(viewer.UserID), nullableText(statusParam), nullableText(filter.CategoryID),
		nullableText(filter.Country), nullableText(filter.Region), nullableText(filter.Query)}

	var total int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM core.tb_place p"+predicate, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting places: %w", err)
	}

	offset := (page - 1) * limit
	rows, err := s.pool.Query(ctx, placeColumns+predicate+`
ORDER BY p.dt_created_at DESC, p.co_place
LIMIT $8 OFFSET $9`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("selecting places: %w", err)
	}
	defer rows.Close()

	result := make([]Place, 0, limit)
	for rows.Next() {
		place, err := scanPlace(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scanning place: %w", err)
		}
		result = append(result, place)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("during place iteration: %w", err)
	}
	return result, total, nil
}

// Get reads one place scoped to the viewer.
func (s *PostgresStore) Get(ctx context.Context, viewer Viewer, placeID string) (Place, error) {
	row := s.pool.QueryRow(ctx, placeColumns+`
WHERE p.co_place = $3::uuid AND `+scopePredicate, viewer.ManageAll, uuidOrNil(viewer.UserID), placeID)
	place, err := scanPlace(row)
	if err != nil {
		return Place{}, ErrNotFound
	}
	return place, nil
}

// Create inserts a place, announcing the actor first so the history trigger records it (S6).
func (s *PostgresStore) Create(ctx context.Context, actorID string, in CreateInput) (Place, error) {
	// co_place has no DEFAULT in the model, so the identifier is generated here rather than
	// assumed: an INSERT that omits it fails with 23502, which is a 500 the caller cannot
	// understand.
	const query = `
INSERT INTO core.tb_place
    (co_place, co_owner_user, tx_name, tx_title, tx_description, co_place_category, tx_country,
     tx_region, tx_timezone, nu_latitude, nu_longitude, tx_address, dt_created_at, dt_updated_at)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, now(), now())
RETURNING co_place::text`

	var placeID string
	err := s.withActor(ctx, actorID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, query,
			in.OwnerUserID, in.Name, in.Title, in.Description, in.CategoryID, in.Country,
			in.Region, in.Timezone, in.Latitude, in.Longitude, in.Address).Scan(&placeID)
	})
	if err != nil {
		return Place{}, fmt.Errorf("inserting place: %w", err)
	}
	return s.Get(ctx, Viewer{ManageAll: true}, placeID)
}

// Update applies a partial change. A row the viewer cannot see is ErrNotFound: the update is
// scoped with the same predicate as the read, so an invisible row is updated zero times.
func (s *PostgresStore) Update(ctx context.Context, viewer Viewer, actorID, placeID string, in UpdateInput) (Place, error) {
	const query = `
UPDATE core.tb_place p SET
    tx_name            = coalesce($4, tx_name),
    tx_title           = coalesce($5, tx_title),
    tx_description     = CASE WHEN $6::boolean THEN $7 ELSE tx_description END,
    co_place_category  = coalesce($8::uuid, co_place_category),
    tx_country         = coalesce($9::char(2), tx_country),
    tx_region          = CASE WHEN $10::boolean THEN $11 ELSE tx_region END,
    tx_timezone        = coalesce($12, tx_timezone),
    nu_latitude        = coalesce($13, nu_latitude),
    nu_longitude       = coalesce($14, nu_longitude),
    tx_address         = CASE WHEN $15::boolean THEN $16 ELSE tx_address END,
    co_owner_user      = CASE WHEN $17::boolean THEN $18::uuid ELSE co_owner_user END,
    tx_status          = coalesce($19::public.suspendable_status, tx_status),
    dt_updated_at      = now()
WHERE p.co_place = $3::uuid
  AND ` + scopePredicate + `
RETURNING p.co_place::text`

	var touched string
	err := s.withActor(ctx, actorID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, query,
			viewer.ManageAll, uuidOrNil(viewer.UserID), placeID,
			in.Name, in.Title, in.Description != nil, derefStringPtr(in.Description),
			in.CategoryID, in.Country,
			in.Region != nil, derefStringPtr(in.Region),
			in.Timezone, in.Latitude, in.Longitude,
			in.Address != nil, derefStringPtr(in.Address),
			in.OwnerUserID != nil, derefStringPtr(in.OwnerUserID),
			in.Status,
		).Scan(&touched)
	})
	if err != nil {
		return Place{}, ErrNotFound
	}
	return s.Get(ctx, viewer, placeID)
}

// Retire is the logical delete (S5): the row stays, the public read paths stop serving it.
func (s *PostgresStore) Retire(ctx context.Context, viewer Viewer, actorID, placeID string) error {
	const query = `
UPDATE core.tb_place p SET dt_deleted_at = now(), dt_updated_at = now()
WHERE p.co_place = $3::uuid AND p.dt_deleted_at IS NULL AND ` + scopePredicate
	err := s.withActor(ctx, actorID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, query, viewer.ManageAll, uuidOrNil(viewer.UserID), placeID)
		return err
	})
	if err != nil {
		return fmt.Errorf("retiring place: %w", err)
	}
	return nil
}

// CategoryExists reports whether the category is live.
func (s *PostgresStore) CategoryExists(ctx context.Context, categoryID string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM core.tb_place_category
			WHERE co_place_category = $1::uuid AND dt_deleted_at IS NULL
		)`, categoryID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("checking category: %w", err)
	}
	return exists, nil
}

// Categories reads the catalogue, newest last so the seeded vocabulary keeps a stable order.
func (s *PostgresStore) Categories(ctx context.Context, status string, page, limit int) ([]Category, int, error) {
	const predicate = `
WHERE dt_deleted_at IS NULL
  AND ($1::public.activatable_status IS NULL OR tx_status = $1::public.activatable_status)`

	// INACTIVE is a value of the enum, so it binds; an unknown status is rejected upstream.
	var statusParam any
	if status != "" {
		statusParam = status
	}

	var total int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM core.tb_place_category"+predicate, statusParam).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting categories: %w", err)
	}

	offset := (page - 1) * limit
	rows, err := s.pool.Query(ctx, `
SELECT co_place_category::text, tx_code, tx_name, tx_status::text
FROM core.tb_place_category`+predicate+`
ORDER BY tx_name
LIMIT $2 OFFSET $3`, statusParam, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("listing categories: %w", err)
	}
	defer rows.Close()

	result := make([]Category, 0, limit)
	for rows.Next() {
		var category Category
		if err := rows.Scan(&category.ID, &category.Code, &category.Name, &category.Status); err != nil {
			return nil, 0, fmt.Errorf("scanning category: %w", err)
		}
		result = append(result, category)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("during category iteration: %w", err)
	}
	return result, total, nil
}

// withActor runs one write with the caller announced, so the tb_place history trigger records
// co_actor_user (S6). The service never inserts a history row itself.
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

// scanPlace reads one row of placeColumns.
func scanPlace(row interface{ Scan(dest ...any) error }) (Place, error) {
	var place Place
	err := row.Scan(&place.ID, &place.OwnerUserID, &place.Name, &place.Title, &place.Description,
		&place.CategoryID, &place.CategoryName, &place.Country, &place.Region, &place.Timezone,
		&place.Latitude, &place.Longitude, &place.Address, &place.Status,
		&place.CreatedAt, &place.UpdatedAt, &place.DeletedAt)
	return place, err
}

// uuidOrNil turns an empty identifier into NULL: binding "" to a uuid parameter is an
// invalid cast, which would surface as "not found" and hide a real bug.
func uuidOrNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func derefStringPtr(value **string) any {
	if value == nil || *value == nil {
		return nil
	}
	return **value
}
