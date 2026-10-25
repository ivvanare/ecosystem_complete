// Package db owns the PostgreSQL connection and the schema bootstrap.
package db

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// bootstrapFile creates the database itself. A service already connected to the
// database cannot create it, and creating databases is provisioning, not migration.
const bootstrapFile = "01-create-database.sql"

// Migration is one SQL file from the canonical schema directory.
type Migration struct {
	Name string
	SQL  string
}

// Executor runs a SQL batch.
type Executor interface {
	Exec(ctx context.Context, sql string) error
}

// Ledger records which schema files already ran.
type Ledger interface {
	Ensure(ctx context.Context) error
	Applied(ctx context.Context, name string) (bool, error)
	Record(ctx context.Context, name string) error
}

// Runner applies pending schema files from Dir.
type Runner struct {
	Dir      string
	Executor Executor
	Ledger   Ledger
	Logger   *slog.Logger
}

// Run applies every pending schema file in lexical order and returns their names.
// It stops at the first failure, leaving earlier files recorded and the failing one not.
func (r *Runner) Run(ctx context.Context) ([]string, error) {
	if err := r.Ledger.Ensure(ctx); err != nil {
		return nil, fmt.Errorf("ensuring migration ledger: %w", err)
	}

	migrations, err := Load(r.Dir)
	if err != nil {
		return nil, err
	}

	applied := make([]string, 0, len(migrations))
	for _, migration := range migrations {
		done, err := r.Ledger.Applied(ctx, migration.Name)
		if err != nil {
			return applied, fmt.Errorf("checking schema file %s: %w", migration.Name, err)
		}
		if done {
			continue
		}

		if err := r.Executor.Exec(ctx, migration.SQL); err != nil {
			return applied, fmt.Errorf("applying schema file %s: %w", migration.Name, err)
		}
		if err := r.Ledger.Record(ctx, migration.Name); err != nil {
			return applied, fmt.Errorf("recording schema file %s: %w", migration.Name, err)
		}

		applied = append(applied, migration.Name)
		if r.Logger != nil {
			r.Logger.Info("schema file applied", slog.String("file", migration.Name))
		}
	}

	return applied, nil
}

// Load reads the schema directory, keeping NN-*.sql files in lexical order.
func Load(dir string) ([]Migration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading schema directory %s: %w", dir, err)
	}

	migrations := make([]Migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !isSchemaFile(name) || name == bootstrapFile {
			continue
		}

		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("reading schema file %s: %w", name, err)
		}
		sql := stripMetaCommands(string(raw))
		if strings.TrimSpace(sql) == "" {
			continue
		}

		migrations = append(migrations, Migration{Name: name, SQL: sql})
	}

	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Name < migrations[j].Name })
	return migrations, nil
}

// isSchemaFile reports whether name matches the NN-*.sql convention.
func isSchemaFile(name string) bool {
	if !strings.HasSuffix(name, ".sql") || len(name) < 5 {
		return false
	}
	first, second := name[0], name[1]
	return first >= '0' && first <= '9' && second >= '0' && second <= '9' && name[2] == '-'
}

// stripMetaCommands drops psql directives such as `\connect melzone`. They are
// interpreted by the psql client, not by the server, so they are invalid SQL.
func stripMetaCommands(sql string) string {
	lines := strings.Split(sql, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), `\`) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}
