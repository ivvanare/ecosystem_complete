package db

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connection retry defaults. Bounded on purpose: a wedged database should surface as a
// failing container instead of an infinite silent wait.
const (
	DefaultConnectAttempts = 5
	defaultConnectBackoff  = 500 * time.Millisecond
	maxConnectBackoff      = 2 * time.Second
)

// Connect opens a pool and waits until PostgreSQL answers or the attempts run out.
func Connect(ctx context.Context, dsn string, attempts int, logger *slog.Logger) (*pgxpool.Pool, error) {
	var pool *pgxpool.Pool

	err := retry(ctx, attempts, defaultConnectBackoff, func(ctx context.Context) error {
		candidate, err := pgxpool.New(ctx, dsn)
		if err != nil {
			return err
		}
		if err := candidate.Ping(ctx); err != nil {
			candidate.Close()
			return err
		}
		pool = candidate
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("connecting to postgres (DATABASE_URL): %w", err)
	}

	if logger != nil {
		logger.Info("postgres pool ready")
	}
	return pool, nil
}

// retry runs op until it succeeds, attempts are exhausted, or ctx is cancelled.
func retry(ctx context.Context, attempts int, baseBackoff time.Duration, op func(context.Context) error) error {
	if attempts < 1 {
		attempts = 1
	}

	backoff := baseBackoff
	var lastErr error

	for attempt := 0; attempt < attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		if err := op(ctx); err != nil {
			lastErr = err
		} else {
			return nil
		}

		if attempt == attempts-1 {
			break
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > maxConnectBackoff {
			backoff = maxConnectBackoff
		}
	}

	return fmt.Errorf("gave up after %d attempts: %w", attempts, lastErr)
}

// PoolExecutor adapts a pgx pool to the Executor interface. pgx sends multi-statement
// batches through the simple query protocol when no arguments are given, which is why
// schema files do not need statement splitting.
type PoolExecutor struct {
	Pool *pgxpool.Pool
}

// Exec runs a SQL batch.
func (e PoolExecutor) Exec(ctx context.Context, sql string) error {
	_, err := e.Pool.Exec(ctx, sql)
	return err
}

// PostgresLedger stores applied schema files in seg.tb_schema_migration.
type PostgresLedger struct {
	Pool *pgxpool.Pool
}

// NewPostgresLedger returns a ledger backed by the given pool.
func NewPostgresLedger(pool *pgxpool.Pool) *PostgresLedger {
	return &PostgresLedger{Pool: pool}
}

const ensureLedgerSQL = `CREATE SCHEMA IF NOT EXISTS seg;
CREATE TABLE IF NOT EXISTS seg.tb_schema_migration (
    tx_name       text        PRIMARY KEY,
    dt_applied_at timestamptz NOT NULL DEFAULT now()
);`

// Ensure creates the ledger table when it does not exist yet.
func (l *PostgresLedger) Ensure(ctx context.Context) error {
	_, err := l.Pool.Exec(ctx, ensureLedgerSQL)
	return err
}

// Applied reports whether the schema file already ran.
func (l *PostgresLedger) Applied(ctx context.Context, name string) (bool, error) {
	var exists bool
	err := l.Pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM seg.tb_schema_migration WHERE tx_name = $1)`, name,
	).Scan(&exists)
	return exists, err
}

// Count reports how many schema files the ledger holds.
func (l *PostgresLedger) Count(ctx context.Context) (int, error) {
	var count int
	err := l.Pool.QueryRow(ctx, `SELECT count(*) FROM seg.tb_schema_migration`).Scan(&count)
	return count, err
}

// SchemaPresent reports whether seg.tb_user exists, which only the base model creates.
func (l *PostgresLedger) SchemaPresent(ctx context.Context) (bool, error) {
	var exists bool
	err := l.Pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_tables WHERE schemaname = 'seg' AND tablename = 'tb_user'
		)`,
	).Scan(&exists)
	return exists, err
}

// Adopt records every schema file as applied without executing it.
func (l *PostgresLedger) Adopt(ctx context.Context, names []string) error {
	if len(names) == 0 {
		return nil
	}
	_, err := l.Pool.Exec(ctx, `
		INSERT INTO seg.tb_schema_migration (tx_name)
		SELECT unnest($1::text[])
		ON CONFLICT (tx_name) DO NOTHING`,
		names,
	)
	return err
}

// Record marks the schema file as applied.
func (l *PostgresLedger) Record(ctx context.Context, name string) error {
	_, err := l.Pool.Exec(ctx,
		`INSERT INTO seg.tb_schema_migration (tx_name) VALUES ($1) ON CONFLICT (tx_name) DO NOTHING`,
		name,
	)
	return err
}
