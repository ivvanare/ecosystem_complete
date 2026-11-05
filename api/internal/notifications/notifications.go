// Package notifications delivers the notifications a user can enumerate and disable (S13).
//
// Delivery is email through the queue the service already owns: push waits for the mobile
// client, and a channel nobody can receive is not a channel. Every kind is individually
// disableable, and absence of a preference row means enabled, so a new kind needs no backfill.
package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ivvanare/ecosystem_complete/api/internal/mailer"
)

// The catalogue of kinds. It is a closed set in code — a client may enumerate it and toggle
// each one — and a dotted convention in storage, because the catalogue grows with features.
const (
	// KindInvitationReceived tells an invitee that somebody invited them to a plan.
	KindInvitationReceived = "invitation.received"
)

// Kinds is the catalogue in the order a client should present it.
var Kinds = []string{KindInvitationReceived}

// IsKnownKind reports whether the catalogue carries this kind. An unknown kind is a
// validation failure rather than a row nobody will ever read.
func IsKnownKind(kind string) bool {
	for _, known := range Kinds {
		if known == kind {
			return true
		}
	}
	return false
}

// Preferences is one user's toggles, resolved against the catalogue so a client can render
// the whole list without guessing.
type Preferences struct {
	Disabled []string
}

// Store is the persistence seam.
type Store interface {
	Disabled(ctx context.Context, userID string) ([]string, error)
	SetDisabled(ctx context.Context, userID, kind string, disabled bool) error
	Contact(ctx context.Context, userID string) (email string, name string, err error)
}

// Publisher is the mail queue.
type Publisher interface {
	Publish(ctx context.Context, payload []byte) error
}

// Service renders preferences and delivers notifications.
type Service struct {
	store     Store
	publisher Publisher
	logger    *slog.Logger
}

// NewService wires the domain.
func NewService(store Store, publisher Publisher, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{store: store, publisher: publisher, logger: logger}
}

// Preferences returns the caller's toggles.
func (s *Service) Preferences(ctx context.Context, userID string) (Preferences, error) {
	disabled, err := s.store.Disabled(ctx, userID)
	if err != nil {
		return Preferences{}, err
	}
	return Preferences{Disabled: disabled}, nil
}

// SetPreference toggles one kind. The kind must be in the catalogue: an unknown one would be a
// row nobody reads and a switch that lies.
func (s *Service) SetPreference(ctx context.Context, userID, kind string, disabled bool) error {
	if !IsKnownKind(kind) {
		return fmt.Errorf("notifications: unknown kind %q", kind)
	}
	return s.store.SetDisabled(ctx, userID, kind, disabled)
}

// InvitationReceived notifies an invitee that a plan invitation arrived.
//
// Delivery is best effort and never blocks the invitation: if the user disabled the kind, or
// the queue is down, the invitation still exists. A notification is a courtesy, and a courtesy
// must not be able to fail the thing it announces.
func (s *Service) InvitationReceived(ctx context.Context, userID, planName string) {
	disabled, err := s.store.Disabled(ctx, userID)
	if err != nil {
		s.logger.Warn("notification skipped: preferences unavailable", slog.String("error", err.Error()))
		return
	}
	for _, kind := range disabled {
		if kind == KindInvitationReceived {
			return
		}
	}

	email, name, err := s.store.Contact(ctx, userID)
	if err != nil || email == "" {
		s.logger.Warn("notification skipped: no contact for the invitee", slog.String("error", errString(err)))
		return
	}

	greeting := "Hola"
	if name != "" {
		greeting = "Hola " + name
	}
	message := mailer.Message{
		To:      email,
		Subject: "Te invitaron a un plan",
		Text: fmt.Sprintf("%s,\n\nTe invitaron al plan %q. Abrí la app para ver los detalles y responder.\n",
			greeting, planName),
	}
	payload, err := json.Marshal(message)
	if err != nil {
		s.logger.Warn("notification skipped: encoding failed", slog.String("error", err.Error()))
		return
	}
	if err := s.publisher.Publish(ctx, payload); err != nil {
		s.logger.Warn("notification skipped: queue unavailable", slog.String("error", err.Error()))
		return
	}
	s.logger.Info("notification queued", slog.String("kind", KindInvitationReceived))
}

func errString(err error) string {
	if err == nil {
		return "none"
	}
	return err.Error()
}

// PostgresStore is the pgx implementation.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore wires the store.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

// Disabled returns the kinds this user turned off.
func (s *PostgresStore) Disabled(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT tx_kind FROM core.tb_notification_optout
		 WHERE co_user = $1::uuid ORDER BY tx_kind`, userID)
	if err != nil {
		return nil, fmt.Errorf("reading preferences: %w", err)
	}
	defer rows.Close()

	result := make([]string, 0, 4)
	for rows.Next() {
		var kind string
		if err := rows.Scan(&kind); err != nil {
			return nil, fmt.Errorf("scanning preference: %w", err)
		}
		result = append(result, kind)
	}
	return result, rows.Err()
}

// SetDisabled inserts or removes the opt-out row: absence means enabled.
func (s *PostgresStore) SetDisabled(ctx context.Context, userID, kind string, disabled bool) error {
	if disabled {
		_, err := s.pool.Exec(ctx, `
			INSERT INTO core.tb_notification_optout (co_user, tx_kind, dt_created_at)
			VALUES ($1::uuid, $2, now())
			ON CONFLICT (co_user, tx_kind) DO NOTHING`, userID, kind)
		if err != nil {
			return fmt.Errorf("disabling a kind: %w", err)
		}
		return nil
	}
	if _, err := s.pool.Exec(ctx, `
		DELETE FROM core.tb_notification_optout WHERE co_user = $1::uuid AND tx_kind = $2`,
		userID, kind); err != nil {
		return fmt.Errorf("enabling a kind: %w", err)
	}
	return nil
}

// Contact returns the address and display name a notification would use.
func (s *PostgresStore) Contact(ctx context.Context, userID string) (string, string, error) {
	var email, name string
	err := s.pool.QueryRow(ctx, `
		SELECT tx_email, tx_name FROM seg.tb_user
		 WHERE co_user = $1::uuid AND dt_deleted_at IS NULL`, userID).Scan(&email, &name)
	if err != nil {
		return "", "", fmt.Errorf("reading the invitee's contact: %w", err)
	}
	return email, name, nil
}
