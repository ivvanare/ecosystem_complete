// Package reviews implements the reviews write path (p1-discovery.md §6): one active
// review per user and place enforced by the partial unique index and surfaced as a
// conflict, author-only mutations with a logical delete, moderation over the seeded
// review.moderate capability, and the actor announced per transaction so the history
// trigger records co_actor_user. The service never writes core.tb_review_history.
package reviews

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// The existing review_status enum (02-schemas-enums.sql). Moderation reuses it: no new
// value is invented (S12).
const (
	StatusActive    = "ACTIVE"
	StatusInactive  = "INACTIVE"
	StatusModerated = "MODERATED"
)

var (
	// ErrNotFound covers a soft-deleted or INACTIVE place and a missing or deleted
	// review alike, so retired places cannot be enumerated through error differences
	// (S14).
	ErrNotFound = errors.New("reviews: not found")
	// ErrForbidden is the author-only rule of S11: the caller is not the review's
	// author.
	ErrForbidden = errors.New("reviews: the caller is not the author")
	// ErrAlreadyExists is the partial unique index ux_tb_review_user_place_active
	// surfacing as a conflict (S10): one active review per user and place.
	ErrAlreadyExists = errors.New("reviews: an active review already exists for this user and place")
)

// Review is one review with the author's display-safe fields. The email never leaves
// the store.
type Review struct {
	ID         string
	PlaceID    string
	AuthorID   string
	AuthorName *string
	Rating     int
	Comment    *string
	Status     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Viewer is who asks for the list (S11): anonymous when UserID is empty, otherwise
// the author of their own rows; Moderator sees everything.
type Viewer struct {
	UserID    string
	Moderator bool
}

// CreateInput is a validated new review.
type CreateInput struct {
	Rating  int
	Comment *string
}

// UpdateInput is a validated edit; nil fields stay untouched.
type UpdateInput struct {
	Rating  *int
	Comment *string
}

// ListResult is one page of reviews.
type ListResult struct {
	Rows  []Review
	Total int
	Page  int
	Limit int
}

// Store is the persistence seam. The Postgres implementation announces the actor per
// write transaction and maps the partial unique to ErrAlreadyExists; tests fake it,
// because the suite never needs PostgreSQL (AGENTS.md).
type Store interface {
	PlaceActive(ctx context.Context, placeID string) (bool, error)
	List(ctx context.Context, placeID string, viewer Viewer, page, limit int) (rows []Review, total int, err error)
	Get(ctx context.Context, placeID, reviewID string) (Review, error)
	Create(ctx context.Context, actorID, placeID string, in CreateInput) (Review, error)
	Update(ctx context.Context, actorID, reviewID string, in UpdateInput) (Review, error)
	Delete(ctx context.Context, actorID, reviewID string) error
	Moderate(ctx context.Context, actorID, reviewID, status string) (Review, error)
}

// Service orchestrates the rules that span calls: place liveness (S14) and the
// author-only mutations (S11).
type Service struct {
	store  Store
	logger *slog.Logger
}

// NewService wires the reviews path.
func NewService(store Store, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{store: store, logger: logger}
}

// requirePlace enforces S14: a soft-deleted or INACTIVE place is not-found on every
// place-scoped review route.
func (s *Service) requirePlace(ctx context.Context, placeID string) error {
	active, err := s.store.PlaceActive(ctx, placeID)
	if err != nil {
		return fmt.Errorf("checking place: %w", err)
	}
	if !active {
		return ErrNotFound
	}
	return nil
}

// List returns one page of reviews visible to the viewer.
func (s *Service) List(ctx context.Context, placeID string, viewer Viewer, page, limit int) (ListResult, error) {
	if err := s.requirePlace(ctx, placeID); err != nil {
		return ListResult{}, err
	}
	rows, total, err := s.store.List(ctx, placeID, viewer, page, limit)
	if err != nil {
		return ListResult{}, err
	}
	return ListResult{Rows: rows, Total: total, Page: page, Limit: limit}, nil
}

// Create inserts the caller's review. The conflict is the database's, mapped by the
// store: checking first would race (S10).
func (s *Service) Create(ctx context.Context, actorID, placeID string, in CreateInput) (Review, error) {
	if err := s.requirePlace(ctx, placeID); err != nil {
		return Review{}, err
	}
	return s.store.Create(ctx, actorID, placeID, in)
}

// requireAuthor loads the live review and rejects anyone but its author (S11).
func (s *Service) requireAuthor(ctx context.Context, actorID, placeID, reviewID string) error {
	if err := s.requirePlace(ctx, placeID); err != nil {
		return err
	}
	review, err := s.store.Get(ctx, placeID, reviewID)
	if err != nil {
		return err
	}
	if review.AuthorID != actorID {
		return ErrForbidden
	}
	return nil
}

// Update edits the caller's own review.
func (s *Service) Update(ctx context.Context, actorID, placeID, reviewID string, in UpdateInput) (Review, error) {
	if err := s.requireAuthor(ctx, actorID, placeID, reviewID); err != nil {
		return Review{}, err
	}
	return s.store.Update(ctx, actorID, reviewID, in)
}

// Delete logically deletes the caller's own review, so the partial index frees the
// pair and the author may review the place again (S11).
func (s *Service) Delete(ctx context.Context, actorID, placeID, reviewID string) error {
	if err := s.requireAuthor(ctx, actorID, placeID, reviewID); err != nil {
		return err
	}
	return s.store.Delete(ctx, actorID, reviewID)
}

// Moderate is not place-scoped: the admin route addresses the review directly (S12).
func (s *Service) Moderate(ctx context.Context, actorID, reviewID, status string) (Review, error) {
	return s.store.Moderate(ctx, actorID, reviewID, status)
}
