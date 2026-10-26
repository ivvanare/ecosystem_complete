// Command bootstrap grants a role to an existing user.
//
// It exists because of a chicken-and-egg problem: assigning roles needs admin.access, and
// nobody holds it until someone is granted a role. Run it once, against a real database, to
// create the first administrator:
//
//	DATABASE_URL=... EMAIL=ana@melia.test ROLE=SUPERADMIN bootstrap
//
// It REPLACES the role set of that user, like PATCH /v1/admin/users/{id}/roles does.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ivvanare/ecosystem_complete/api/internal/identity"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "bootstrap failed: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dsn == "" {
		return errors.New("missing required environment variable: DATABASE_URL")
	}
	email := strings.ToLower(strings.TrimSpace(os.Getenv("EMAIL")))
	if email == "" {
		return errors.New("missing required environment variable: EMAIL")
	}
	role := valueOr("ROLE", "SUPERADMIN")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connecting: %w", err)
	}
	defer pool.Close()

	var userID string
	err = pool.QueryRow(ctx, `
		SELECT co_user::text FROM seg.tb_user
		WHERE tx_email = $1 AND dt_deleted_at IS NULL`, email).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("no active user with email %s: register first, then bootstrap", email)
	}
	if err != nil {
		return fmt.Errorf("looking up user: %w", err)
	}

	roles, err := identity.NewPostgresStore(pool).SetRoles(ctx, userID, []string{role})
	if err != nil {
		return err
	}

	fmt.Printf("granted %v to %s (%s)\n", roles, email, userID)
	return nil
}

func valueOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
