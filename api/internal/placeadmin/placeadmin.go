// Package placeadmin is the places backoffice: the routes an operator uses to curate the
// catalogue, with an ownership viewer that decides who may see and change what (D21).
package placeadmin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
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
	// Schedules and Attributes are populated by the detail read only: a page of places does
	// not need them, and fetching them per row would be a query per row.
	Schedules  []Schedule
	Attributes []string
	Menus      []Menu
	CreatedAt  time.Time
	UpdatedAt  time.Time
	DeletedAt  *time.Time
}

// Schedule is one weekly opening interval. A close time at or before the open time crosses
// midnight, which is how the model already expresses a bar open 20:00-02:00.
type Schedule struct {
	DayOfWeek int    `json:"dayOfWeek"`
	Open      string `json:"open"`
	Close     string `json:"close"`
}

// MenuItem is one dish with its price. The currency is mandatory: a number without one does
// not mean anything, which is why the model refuses it too.
type MenuItem struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Price       float64 `json:"price"`
	Currency    string  `json:"currency"`
}

// Menu is one section of the card (breakfast, dinner, ...).
type Menu struct {
	Name        string     `json:"name"`
	Description *string    `json:"description"`
	Items       []MenuItem `json:"items"`
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
	Schedules(ctx context.Context, placeID string) ([]Schedule, error)
	ReplaceSchedules(ctx context.Context, actorID, placeID string, schedules []Schedule) error
	Attributes(ctx context.Context, placeID string) ([]string, error)
	ReplaceAttributes(ctx context.Context, actorID, placeID string, codes []string) error
	UnknownAttributes(ctx context.Context, codes []string) ([]string, error)
	Menus(ctx context.Context, placeID string) ([]Menu, error)
	ReplaceMenus(ctx context.Context, actorID, placeID string, menus []Menu) error
	UnknownCurrencies(ctx context.Context, codes []string) ([]string, error)
	AttributeCatalogue(ctx context.Context) ([]Category, error)
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

// Schedules returns the weekly hours of a place the viewer may see.
func (s *Service) Schedules(ctx context.Context, viewer Viewer, placeID string) ([]Schedule, error) {
	if _, err := s.store.Get(ctx, viewer, placeID); err != nil {
		return nil, err
	}
	return s.store.Schedules(ctx, placeID)
}

// ReplaceSchedules replaces the whole weekly set.
//
// Replacement rather than per-slot editing is deliberate: an operator edits "the hours of this
// place", and a half-applied weekly set is worse than none — it would answer "closed" for a day
// nobody touched.
func (s *Service) ReplaceSchedules(ctx context.Context, viewer Viewer, actorID, placeID string, schedules []Schedule) error {
	if _, err := s.store.Get(ctx, viewer, placeID); err != nil {
		return err
	}
	if err := validateSchedules(schedules); err != nil {
		return err
	}
	return s.store.ReplaceSchedules(ctx, actorID, placeID, schedules)
}

// Attributes returns the codes present on a place.
func (s *Service) Attributes(ctx context.Context, viewer Viewer, placeID string) ([]string, error) {
	if _, err := s.store.Get(ctx, viewer, placeID); err != nil {
		return nil, err
	}
	return s.store.Attributes(ctx, placeID)
}

// ReplaceAttributes replaces the whole set, refusing codes the catalogue does not carry.
func (s *Service) ReplaceAttributes(ctx context.Context, viewer Viewer, actorID, placeID string, codes []string) error {
	if _, err := s.store.Get(ctx, viewer, placeID); err != nil {
		return err
	}
	unknown, err := s.store.UnknownAttributes(ctx, codes)
	if err != nil {
		return err
	}
	if len(unknown) > 0 {
		return &UnknownAttributeError{Codes: unknown}
	}
	return s.store.ReplaceAttributes(ctx, actorID, placeID, codes)
}

// UnknownAttributeError names the codes the catalogue does not carry, so the transport can
// reject the request instead of silently dropping an attribute the operator believes is set.
type UnknownAttributeError struct{ Codes []string }

func (e *UnknownAttributeError) Error() string {
	return fmt.Sprintf("placeadmin: unknown attributes %v", e.Codes)
}

// validateSchedules enforces the model's shapes at the edge.
func validateSchedules(schedules []Schedule) error {
	for _, schedule := range schedules {
		if schedule.DayOfWeek < 1 || schedule.DayOfWeek > 7 {
			return fmt.Errorf("placeadmin: day of week out of range")
		}
		opens, err := time.Parse("15:04", schedule.Open)
		if err != nil {
			return fmt.Errorf("placeadmin: invalid open time %q", schedule.Open)
		}
		closes, err := time.Parse("15:04", schedule.Close)
		if err != nil {
			return fmt.Errorf("placeadmin: invalid close time %q", schedule.Close)
		}
		// Equal times are ambiguous: it reads as closed all day or open all day depending on
		// who is asking. Rejecting is the only honest option.
		if opens.Equal(closes) {
			return fmt.Errorf("placeadmin: open and close are the same time")
		}
	}
	return nil
}

// AttributeCatalogue returns the attribute vocabulary. It reuses the Category shape: a code, a
// display name and a status are all a select needs, and the panel and the mobile filters need
// exactly the same list.
func (s *Service) AttributeCatalogue(ctx context.Context) ([]Category, error) {
	return s.store.AttributeCatalogue(ctx)
}

// Menus returns the card of a place.
func (s *Service) Menus(ctx context.Context, viewer Viewer, placeID string) ([]Menu, error) {
	if _, err := s.store.Get(ctx, viewer, placeID); err != nil {
		return nil, err
	}
	return s.store.Menus(ctx, placeID)
}

// ReplaceMenus replaces the whole card, refusing a currency the catalogue does not carry and an
// unknown dish price.
//
// Replacement, like the weekly hours: an operator edits "the card of this place", and a menu
// half-applied would show prices that a filter reads and a customer never sees.
func (s *Service) ReplaceMenus(ctx context.Context, viewer Viewer, actorID, placeID string, menus []Menu) error {
	if _, err := s.store.Get(ctx, viewer, placeID); err != nil {
		return err
	}
	if err := validateMenus(menus); err != nil {
		return err
	}
	currencies := make([]string, 0, 4)
	for _, menu := range menus {
		for _, item := range menu.Items {
			currencies = append(currencies, item.Currency)
		}
	}
	unknown, err := s.store.UnknownCurrencies(ctx, currencies)
	if err != nil {
		return err
	}
	if len(unknown) > 0 {
		return &UnknownCurrencyError{Codes: unknown}
	}
	return s.store.ReplaceMenus(ctx, actorID, placeID, menus)
}

// UnknownCurrencyError names the currency codes the catalogue does not carry.
type UnknownCurrencyError struct{ Codes []string }

func (e *UnknownCurrencyError) Error() string {
	return fmt.Sprintf("placeadmin: unknown currencies %v", e.Codes)
}

// validateMenus enforces the shapes the model enforces, so a bad row is a 422 and not a 500.
func validateMenus(menus []Menu) error {
	for _, menu := range menus {
		if strings.TrimSpace(menu.Name) == "" {
			return fmt.Errorf("placeadmin: a menu needs a name")
		}
		for _, item := range menu.Items {
			if strings.TrimSpace(item.Name) == "" {
				return fmt.Errorf("placeadmin: an item needs a name")
			}
			// The model has CHECK (nu_price >= 0); catching it here keeps a negative price a
			// validation failure instead of a constraint violation the client cannot read.
			if item.Price < 0 {
				return fmt.Errorf("placeadmin: negative price")
			}
			if strings.TrimSpace(item.Currency) == "" {
				return fmt.Errorf("placeadmin: an item needs a currency")
			}
		}
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
	// The detail read carries the weekly hours and the attributes: an operator cannot edit what
	// the form does not show, and fetching them per list row would be a query per row.
	if place.Schedules, err = s.Schedules(ctx, place.ID); err != nil {
		return Place{}, err
	}
	if place.Attributes, err = s.Attributes(ctx, place.ID); err != nil {
		return Place{}, err
	}
	if place.Menus, err = s.Menus(ctx, place.ID); err != nil {
		return Place{}, err
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

// Schedules reads the weekly hours.
func (s *PostgresStore) Schedules(ctx context.Context, placeID string) ([]Schedule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT nu_day_of_week, to_char(dt_open_time, 'HH24:MI'), to_char(dt_close_time, 'HH24:MI')
		FROM core.tb_place_schedule
		WHERE co_place = $1::uuid
		ORDER BY nu_day_of_week, dt_open_time`, placeID)
	if err != nil {
		return nil, fmt.Errorf("reading schedules: %w", err)
	}
	defer rows.Close()

	result := make([]Schedule, 0, 7)
	for rows.Next() {
		var schedule Schedule
		if err := rows.Scan(&schedule.DayOfWeek, &schedule.Open, &schedule.Close); err != nil {
			return nil, fmt.Errorf("scanning schedule: %w", err)
		}
		result = append(result, schedule)
	}
	return result, rows.Err()
}

// ReplaceSchedules replaces the weekly set in one transaction: delete then insert, so a reader
// never sees half the hours.
func (s *PostgresStore) ReplaceSchedules(ctx context.Context, actorID, placeID string, schedules []Schedule) error {
	return s.withActor(ctx, actorID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM core.tb_place_schedule WHERE co_place = $1::uuid`, placeID); err != nil {
			return fmt.Errorf("clearing schedules: %w", err)
		}
		for _, schedule := range schedules {
			if _, err := tx.Exec(ctx, `
				INSERT INTO core.tb_place_schedule
				    (co_schedule, co_place, nu_day_of_week, dt_open_time, dt_close_time, dt_created_at, dt_updated_at)
				VALUES (gen_random_uuid(), $1::uuid, $2, $3::time, $4::time, now(), now())`,
				placeID, schedule.DayOfWeek, schedule.Open, schedule.Close); err != nil {
				return fmt.Errorf("inserting schedule: %w", err)
			}
		}
		return nil
	})
}

// Attributes reads the codes present on a place.
func (s *PostgresStore) Attributes(ctx context.Context, placeID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.tx_code
		FROM core.tb_place_attribute_link l
		JOIN core.tb_place_attribute a ON a.co_attribute = l.co_attribute
		WHERE l.co_place = $1::uuid AND a.dt_deleted_at IS NULL
		ORDER BY a.tx_code`, placeID)
	if err != nil {
		return nil, fmt.Errorf("reading attributes: %w", err)
	}
	defer rows.Close()

	result := make([]string, 0, 4)
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, fmt.Errorf("scanning attribute: %w", err)
		}
		result = append(result, code)
	}
	return result, rows.Err()
}

// UnknownAttributes returns the codes the catalogue does not carry, so the caller can reject
// the request instead of silently dropping what the operator asked for.
func (s *PostgresStore) UnknownAttributes(ctx context.Context, codes []string) ([]string, error) {
	if len(codes) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT code FROM unnest($1::text[]) AS code
		 WHERE NOT EXISTS (
		       SELECT 1 FROM core.tb_place_attribute a
		        WHERE a.tx_code = code AND a.dt_deleted_at IS NULL)`, codes)
	if err != nil {
		return nil, fmt.Errorf("checking attributes: %w", err)
	}
	defer rows.Close()

	unknown := make([]string, 0, 2)
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, fmt.Errorf("scanning unknown attribute: %w", err)
		}
		unknown = append(unknown, code)
	}
	return unknown, rows.Err()
}

// ReplaceAttributes replaces the whole set in one transaction.
func (s *PostgresStore) ReplaceAttributes(ctx context.Context, actorID, placeID string, codes []string) error {
	return s.withActor(ctx, actorID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM core.tb_place_attribute_link WHERE co_place = $1::uuid`, placeID); err != nil {
			return fmt.Errorf("clearing attributes: %w", err)
		}
		for _, code := range codes {
			if _, err := tx.Exec(ctx, `
				INSERT INTO core.tb_place_attribute_link
				    (co_place, co_attribute, dt_created_at, dt_updated_at)
				SELECT $1::uuid, a.co_attribute, now(), now() FROM core.tb_place_attribute a
				 WHERE a.tx_code = $2 AND a.dt_deleted_at IS NULL
				ON CONFLICT DO NOTHING`, placeID, code); err != nil {
				return fmt.Errorf("linking attribute: %w", err)
			}
		}
		return nil
	})
}

// AttributeCatalogue reads the attribute vocabulary.
func (s *PostgresStore) AttributeCatalogue(ctx context.Context) ([]Category, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT co_attribute::text, tx_code, tx_name, tx_status::text
		FROM core.tb_place_attribute
		WHERE dt_deleted_at IS NULL
		ORDER BY tx_name`)
	if err != nil {
		return nil, fmt.Errorf("listing attributes: %w", err)
	}
	defer rows.Close()

	result := make([]Category, 0, 8)
	for rows.Next() {
		var attribute Category
		if err := rows.Scan(&attribute.ID, &attribute.Code, &attribute.Name, &attribute.Status); err != nil {
			return nil, fmt.Errorf("scanning attribute: %w", err)
		}
		result = append(result, attribute)
	}
	return result, rows.Err()
}

// Menus reads the card: sections in order, items in order.
func (s *PostgresStore) Menus(ctx context.Context, placeID string) ([]Menu, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT m.co_menu::text, m.tx_name, m.tx_description,
		       i.tx_name, i.tx_description, i.nu_price::float8, c.tx_code
		FROM core.tb_menu m
		LEFT JOIN core.tb_menu_item i ON i.co_menu = m.co_menu
		                            AND i.dt_deleted_at IS NULL AND i.tx_status = 'ACTIVE'
		LEFT JOIN core.tb_currency c ON c.co_currency = i.co_currency
		WHERE m.co_place = $1::uuid AND m.dt_deleted_at IS NULL AND m.tx_status = 'ACTIVE'
		ORDER BY m.dt_created_at, m.co_menu, i.dt_created_at, i.co_menu_item`, placeID)
	if err != nil {
		return nil, fmt.Errorf("reading menus: %w", err)
	}
	defer rows.Close()

	menus := make([]Menu, 0, 3)
	index := map[string]int{}
	for rows.Next() {
		var menuID string
		var name string
		var description *string
		var itemName, itemDescription, currency *string
		var price *float64

		if err := rows.Scan(&menuID, &name, &description, &itemName, &itemDescription, &price, &currency); err != nil {
			return nil, fmt.Errorf("scanning menu row: %w", err)
		}
		position, seen := index[menuID]
		if !seen {
			menus = append(menus, Menu{Name: name, Description: description, Items: []MenuItem{}})
			position = len(menus) - 1
			index[menuID] = position
		}
		// A section with no items is a real state: the LEFT JOIN yields a row with NULLs.
		if itemName != nil && price != nil && currency != nil {
			menus[position].Items = append(menus[position].Items, MenuItem{
				Name: *itemName, Description: itemDescription, Price: *price, Currency: *currency,
			})
		}
	}
	return menus, rows.Err()
}

// UnknownCurrencies returns the codes the currency catalogue does not carry.
func (s *PostgresStore) UnknownCurrencies(ctx context.Context, codes []string) ([]string, error) {
	if len(codes) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT code FROM unnest($1::text[]) AS code
		 WHERE NOT EXISTS (
		       SELECT 1 FROM core.tb_currency c
		        WHERE c.tx_code = code AND c.dt_deleted_at IS NULL)`, codes)
	if err != nil {
		return nil, fmt.Errorf("checking currencies: %w", err)
	}
	defer rows.Close()

	unknown := make([]string, 0, 2)
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, fmt.Errorf("scanning unknown currency: %w", err)
		}
		unknown = append(unknown, code)
	}
	return unknown, rows.Err()
}

// ReplaceMenus replaces the whole card in one transaction. Menus and their items go together:
// a section without its items, or items without a section, is not a state worth exposing.
func (s *PostgresStore) ReplaceMenus(ctx context.Context, actorID, placeID string, menus []Menu) error {
	return s.withActor(ctx, actorID, func(tx pgx.Tx) error {
		// Items first: they reference the menus being removed.
		if _, err := tx.Exec(ctx, `
			DELETE FROM core.tb_menu_item i USING core.tb_menu m
			 WHERE i.co_menu = m.co_menu AND m.co_place = $1::uuid`, placeID); err != nil {
			return fmt.Errorf("clearing menu items: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM core.tb_menu WHERE co_place = $1::uuid`, placeID); err != nil {
			return fmt.Errorf("clearing menus: %w", err)
		}
		for _, menu := range menus {
			var menuID string
			if err := tx.QueryRow(ctx, `
				INSERT INTO core.tb_menu (co_menu, co_place, tx_name, tx_description, dt_created_at, dt_updated_at)
				VALUES (gen_random_uuid(), $1::uuid, $2, $3, now(), now())
				RETURNING co_menu::text`, placeID, menu.Name, menu.Description).Scan(&menuID); err != nil {
				return fmt.Errorf("inserting menu: %w", err)
			}
			for _, item := range menu.Items {
				if _, err := tx.Exec(ctx, `
					INSERT INTO core.tb_menu_item
					    (co_menu_item, co_menu, tx_name, tx_description, nu_price, co_currency, dt_created_at, dt_updated_at)
					SELECT gen_random_uuid(), $1::uuid, $2, $3, $4, c.co_currency, now(), now()
					  FROM core.tb_currency c
					 WHERE c.tx_code = $5 AND c.dt_deleted_at IS NULL`,
					menuID, item.Name, item.Description, item.Price, item.Currency); err != nil {
					return fmt.Errorf("inserting menu item: %w", err)
				}
			}
		}
		return nil
	})
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
