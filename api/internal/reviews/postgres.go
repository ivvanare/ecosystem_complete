package reviews

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// uniqueViolation is the PostgreSQL code for a unique constraint breach.
const uniqueViolation = "23505"

// activeReviewUnique is the partial unique index that enforces one active review per
// user and place (04-core-tables.sql). Its violation is the conflict, never a
// pre-check and never a 500 (S10).
const activeReviewUnique = "ux_tb_review_user_place_active"

// transaction is the slice of pgx.Tx the writes use; tests record it.
type transaction interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// database is the pgx seam: the pool satisfies it through poolDatabase and tests fake
// it, because the suite never needs PostgreSQL (AGENTS.md).
type database interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (transaction, error)
}

// poolDatabase adapts *pgxpool.Pool to the seam: Begin's pgx.Tx satisfies transaction.
type poolDatabase struct {
	pool *pgxpool.Pool
}

func (p poolDatabase) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return p.pool.Query(ctx, sql, args...)
}

func (p poolDatabase) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return p.pool.QueryRow(ctx, sql, args...)
}

func (p poolDatabase) Begin(ctx context.Context) (transaction, error) {
	return p.pool.Begin(ctx)
}

// PostgresStore persists reviews. Every write runs in a transaction that announces the
// actor first, so the existing history trigger records co_actor_user; the service
// never inserts into core.tb_review_history itself (S13).
type PostgresStore struct {
	db database
}

// NewPostgresStore returns a Store backed by the given pool.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{db: poolDatabase{pool: pool}}
}

// reviewColumns is the projection of one review joined to its author's display-safe
// fields, in scan order. The email never leaves the store.
const reviewColumns = `
SELECT r.co_review, r.co_place, r.co_user, u.tx_name,
       r.nu_rating, r.tx_comment, r.tx_status, r.dt_created_at, r.dt_updated_at
FROM core.tb_review r
JOIN seg.tb_user u ON u.co_user = r.co_user`

// reviewListColumns is the projection of the public list: the same fields as
// reviewColumns plus the viewer-relative highlight. $2 is the asking viewer, so an
// anonymous read (NULL) yields false everywhere by construction rather than by a branch.
const reviewListColumns = `
SELECT r.co_review, r.co_place, r.co_user, u.tx_name,
       r.nu_rating, r.tx_comment, r.tx_status, r.dt_created_at, r.dt_updated_at,
       EXISTS (
           SELECT 1 FROM core.tb_follow f
           WHERE f.co_follower = $2::uuid AND f.co_followed = r.co_user
       ) AS via_followed_author
FROM core.tb_review r
JOIN seg.tb_user u ON u.co_user = r.co_user`

// writeColumns is the RETURNING clause of a write, without the author join: the name
// is read afterwards in the same transaction.
const writeColumns = `
RETURNING co_review, co_place, co_user, nu_rating, tx_comment, tx_status, dt_created_at, dt_updated_at`

// visibilityPredicate is the read rule of S11: everyone sees live ACTIVE rows; the
// author ($2) sees their own in any state; a moderator ($3) sees all. It is shared by
// the count and the page so both agree.
const visibilityPredicate = `
WHERE r.co_place = $1::uuid
  AND (
        (r.dt_deleted_at IS NULL AND r.tx_status = 'ACTIVE')
        OR ($2::uuid IS NOT NULL AND r.co_user = $2::uuid)
        OR $3::boolean
      )
  -- Blocking (Q2): a block hides the content in both directions. A moderator still sees
  -- everything, because moderation is not a social relationship.
  AND ($3::boolean OR $2::uuid IS NULL OR NOT EXISTS (
        SELECT 1 FROM core.tb_block b
         WHERE (b.co_blocker = $2::uuid AND b.co_blocked = r.co_user)
            OR (b.co_blocker = r.co_user AND b.co_blocked = $2::uuid)))`

// adminFilterPredicate narrows the moderator listing. Every clause is optional and the
// zero value lists everything, so the count and the page share one predicate and agree.
const adminFilterPredicate = `
WHERE ($1::public.review_status IS NULL OR r.tx_status = $1::public.review_status)
  AND ($2::uuid IS NULL OR r.co_place = $2::uuid)
  AND ($3::uuid IS NULL OR r.co_user = $3::uuid)`

// AdminList returns one page of every review, newest first, for a moderator.
//
// This is the backoffice listing (S17): without it the moderation route has no entry
// point, because nothing can discover a coReview. It deliberately does not apply the
// public visibility predicate — a moderator sees the rows the public cannot.
// adminReviewColumns is the moderator listing projection: the shared fields plus the place
// title, because a moderator may hold review.moderate without place.update and would
// otherwise render a bare uuid.
const adminReviewColumns = `
SELECT r.co_review, r.co_place, r.co_user, u.tx_name,
       r.nu_rating, r.tx_comment, r.tx_status, r.dt_created_at, r.dt_updated_at,
       p.tx_title
FROM core.tb_review r
JOIN seg.tb_user u ON u.co_user = r.co_user
JOIN core.tb_place p ON p.co_place = r.co_place`

// scanReviewAdmin reads one row of adminReviewColumns.
func scanReviewAdmin(row interface{ Scan(dest ...any) error }) (Review, error) {
	var review Review
	err := row.Scan(&review.ID, &review.PlaceID, &review.AuthorID, &review.AuthorName,
		&review.Rating, &review.Comment, &review.Status, &review.CreatedAt, &review.UpdatedAt,
		&review.PlaceTitle)
	return review, err
}

func (s *PostgresStore) AdminList(ctx context.Context, filter AdminFilter, page, limit int) ([]Review, int, error) {
	args := []any{nullableText(filter.Status), nullableText(filter.PlaceID), nullableText(filter.AuthorID)}

	var total int
	if err := s.db.QueryRow(ctx, "SELECT COUNT(*) FROM core.tb_review r"+adminFilterPredicate, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting admin reviews: %w", err)
	}

	offset := (page - 1) * limit
	rows, err := s.db.Query(ctx, adminReviewColumns+adminFilterPredicate+`
ORDER BY r.dt_created_at DESC, r.co_review DESC
LIMIT $4 OFFSET $5`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("selecting admin reviews: %w", err)
	}
	defer rows.Close()

	result := make([]Review, 0, limit)
	for rows.Next() {
		review, err := scanReviewAdmin(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scanning admin review: %w", err)
		}
		result = append(result, review)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("during admin review iteration: %w", err)
	}
	return result, total, nil
}

// nullableText turns an absent filter into a NULL bind, which the predicate above reads
// as "no filter" rather than an empty-string comparison.
func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// PlaceActive reports whether the place is visible: not soft-deleted and ACTIVE (S14).
func (s *PostgresStore) PlaceActive(ctx context.Context, placeID string) (bool, error) {
	var active bool
	err := s.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM core.tb_place
			WHERE co_place = $1::uuid AND dt_deleted_at IS NULL AND tx_status = 'ACTIVE'
		)`, placeID).Scan(&active)
	if err != nil {
		return false, fmt.Errorf("checking place: %w", err)
	}
	return active, nil
}

// List returns one page of reviews visible to the viewer, newest first, and the total
// under the same predicate. It never aggregates for ordering (S14): the place average
// belongs to the worker.
func (s *PostgresStore) List(ctx context.Context, placeID string, viewer Viewer, page, limit int) ([]Review, int, error) {
	args := []any{placeID, nullableUser(viewer.UserID), viewer.Moderator}

	var total int
	if err := s.db.QueryRow(ctx, "SELECT COUNT(*) FROM core.tb_review r"+visibilityPredicate, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting reviews: %w", err)
	}

	offset := (page - 1) * limit
	rows, err := s.db.Query(ctx, reviewListColumns+visibilityPredicate+`
ORDER BY r.dt_created_at DESC, r.co_review DESC
LIMIT $4 OFFSET $5`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("selecting reviews: %w", err)
	}
	defer rows.Close()

	result := make([]Review, 0, limit)
	for rows.Next() {
		review, err := scanReviewList(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scanning review: %w", err)
		}
		result = append(result, review)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("during review iteration: %w", err)
	}
	return result, total, nil
}

// Get reads one live review of the place, for the author check.
func (s *PostgresStore) Get(ctx context.Context, placeID, reviewID string) (Review, error) {
	review, err := scanReview(s.db.QueryRow(ctx, reviewColumns+`
WHERE r.co_place = $1::uuid AND r.co_review = $2::uuid AND r.dt_deleted_at IS NULL`, placeID, reviewID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Review{}, ErrNotFound
	}
	if err != nil {
		return Review{}, fmt.Errorf("selecting review: %w", err)
	}
	return review, nil
}

// beginWrite opens a write transaction and announces the actor for the history
// trigger (08-history-triggers.sql): set_config with is_local=true is
// transaction-scoped, so every statement of this write records co_actor_user and no
// other session sees it.
func (s *PostgresStore) beginWrite(ctx context.Context, actorID string) (transaction, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.current_user_id', $1, true)`, actorID); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("announcing the actor: %w", err)
	}
	return tx, nil
}

// Create inserts the review. The conflict is the partial unique index: a 23505 on it
// maps to ErrAlreadyExists, never a pre-check and never a 500 (S10).
func (s *PostgresStore) Create(ctx context.Context, actorID, placeID string, in CreateInput) (Review, error) {
	tx, err := s.beginWrite(ctx, actorID)
	if err != nil {
		return Review{}, err
	}
	settled := false
	defer func() {
		if !settled {
			_ = tx.Rollback(ctx)
		}
	}()

	var review Review
	err = tx.QueryRow(ctx, `
		INSERT INTO core.tb_review
			(co_review, co_user, co_place, nu_rating, tx_comment, dt_created_at, dt_updated_at)
		VALUES (gen_random_uuid(), $1::uuid, $2::uuid, $3, $4, now(), now())`+writeColumns,
		actorID, placeID, in.Rating, in.Comment,
	).Scan(&review.ID, &review.PlaceID, &review.AuthorID, &review.Rating, &review.Comment,
		&review.Status, &review.CreatedAt, &review.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == activeReviewUnique {
			return Review{}, ErrAlreadyExists
		}
		return Review{}, fmt.Errorf("inserting review: %w", err)
	}

	if err := readAuthorName(ctx, tx, &review); err != nil {
		return Review{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Review{}, fmt.Errorf("committing review: %w", err)
	}
	settled = true
	return review, nil
}

// Update applies the author's edit. COALESCE keeps the fields the request did not
// send, and the row stays scoped to live reviews: a deleted one is not-found.
func (s *PostgresStore) Update(ctx context.Context, actorID, reviewID string, in UpdateInput) (Review, error) {
	tx, err := s.beginWrite(ctx, actorID)
	if err != nil {
		return Review{}, err
	}
	settled := false
	defer func() {
		if !settled {
			_ = tx.Rollback(ctx)
		}
	}()

	var review Review
	err = tx.QueryRow(ctx, `
		UPDATE core.tb_review
		SET nu_rating = COALESCE($2::int, nu_rating),
		    tx_comment = COALESCE($3::text, tx_comment),
		    dt_updated_at = now()
		WHERE co_review = $1::uuid AND dt_deleted_at IS NULL`+writeColumns,
		reviewID, in.Rating, in.Comment,
	).Scan(&review.ID, &review.PlaceID, &review.AuthorID, &review.Rating, &review.Comment,
		&review.Status, &review.CreatedAt, &review.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Review{}, ErrNotFound
	}
	if err != nil {
		return Review{}, fmt.Errorf("updating review: %w", err)
	}

	if err := readAuthorName(ctx, tx, &review); err != nil {
		return Review{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Review{}, fmt.Errorf("committing review edit: %w", err)
	}
	settled = true
	return review, nil
}

// Delete is logical (dt_deleted_at), never physical: the history trigger records the
// UPDATE, and the partial index frees the (user, place) pair so the author may review
// again (S11).
func (s *PostgresStore) Delete(ctx context.Context, actorID, reviewID string) error {
	tx, err := s.beginWrite(ctx, actorID)
	if err != nil {
		return err
	}
	settled := false
	defer func() {
		if !settled {
			_ = tx.Rollback(ctx)
		}
	}()

	tag, err := tx.Exec(ctx, `
		UPDATE core.tb_review
		SET dt_deleted_at = now(), dt_updated_at = now()
		WHERE co_review = $1::uuid AND dt_deleted_at IS NULL`, reviewID)
	if err != nil {
		return fmt.Errorf("deleting review: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing review delete: %w", err)
	}
	settled = true
	return nil
}

// Moderate sets tx_status to one of the existing review_status values (S12).
func (s *PostgresStore) Moderate(ctx context.Context, actorID, reviewID, status string) (Review, error) {
	tx, err := s.beginWrite(ctx, actorID)
	if err != nil {
		return Review{}, err
	}
	settled := false
	defer func() {
		if !settled {
			_ = tx.Rollback(ctx)
		}
	}()

	var review Review
	err = tx.QueryRow(ctx, `
		UPDATE core.tb_review
		SET tx_status = $2::public.review_status, dt_updated_at = now()
		WHERE co_review = $1::uuid AND dt_deleted_at IS NULL`+writeColumns,
		reviewID, status,
	).Scan(&review.ID, &review.PlaceID, &review.AuthorID, &review.Rating, &review.Comment,
		&review.Status, &review.CreatedAt, &review.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Review{}, ErrNotFound
	}
	if err != nil {
		return Review{}, fmt.Errorf("moderating review: %w", err)
	}

	if err := readAuthorName(ctx, tx, &review); err != nil {
		return Review{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Review{}, fmt.Errorf("committing review moderation: %w", err)
	}
	settled = true
	return review, nil
}

// readAuthorName fills the display-safe author field of a freshly written review. The
// write's RETURNING has no join, so the name is read in the same transaction.
func readAuthorName(ctx context.Context, tx transaction, review *Review) error {
	if err := tx.QueryRow(ctx, `SELECT tx_name FROM seg.tb_user WHERE co_user = $1::uuid`, review.AuthorID).Scan(&review.AuthorName); err != nil {
		return fmt.Errorf("reading author name: %w", err)
	}
	return nil
}

// scanReviewList reads one row of reviewListColumns: the shared fields plus the
// viewer-relative highlight, which exists only on the list projection.
func scanReviewList(row interface{ Scan(dest ...any) error }) (Review, error) {
	var review Review
	err := row.Scan(&review.ID, &review.PlaceID, &review.AuthorID, &review.AuthorName,
		&review.Rating, &review.Comment, &review.Status, &review.CreatedAt, &review.UpdatedAt,
		&review.ViaFollowedAuthor)
	return review, err
}

// scanReview reads one row of reviewColumns.
func scanReview(row interface{ Scan(dest ...any) error }) (Review, error) {
	var review Review
	err := row.Scan(&review.ID, &review.PlaceID, &review.AuthorID, &review.AuthorName,
		&review.Rating, &review.Comment, &review.Status, &review.CreatedAt, &review.UpdatedAt)
	return review, err
}

// nullableUser turns an anonymous viewer into a NULL bind, which the visibility
// predicate reads as "no author".
func nullableUser(userID string) any {
	if userID == "" {
		return nil
	}
	return userID
}
