package db

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeExecutor records the SQL it was asked to run, in order.
type fakeExecutor struct {
	ran  []string
	fail map[string]error
}

func (f *fakeExecutor) Exec(_ context.Context, sql string) error {
	f.ran = append(f.ran, sql)
	if f.fail != nil {
		for needle, err := range f.fail {
			if strings.Contains(sql, needle) {
				return err
			}
		}
	}
	return nil
}

// fakeLedger is an in-memory ledger.
type fakeLedger struct {
	applied     map[string]bool
	recorded    []string
	ensured     int
	failEnsure  error
	failApplied error
}

func newFakeLedger(applied ...string) *fakeLedger {
	l := &fakeLedger{applied: map[string]bool{}}
	for _, name := range applied {
		l.applied[name] = true
	}
	return l
}

func (f *fakeLedger) Ensure(context.Context) error {
	f.ensured++
	return f.failEnsure
}

func (f *fakeLedger) Applied(_ context.Context, name string) (bool, error) {
	if f.failApplied != nil {
		return false, f.failApplied
	}
	return f.applied[name], nil
}

func (f *fakeLedger) Record(_ context.Context, name string) error {
	f.recorded = append(f.recorded, name)
	f.applied[name] = true
	return nil
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
}

func TestLoadKeepsSchemaFilesInLexicalOrder(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"05-foreign-keys.sql":    "ALTER TABLE core.tb_route ADD CONSTRAINT x FOREIGN KEY (co_user) REFERENCES seg.tb_user(co_user);",
		"02-schemas-enums.sql":   "CREATE SCHEMA IF NOT EXISTS seg;",
		"01-create-database.sql": "CREATE DATABASE melzone;",
		"notes.txt":              "not a migration",
		"README.md":              "# docs",
	})

	migrations, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	want := []string{"02-schemas-enums.sql", "05-foreign-keys.sql"}
	if len(migrations) != len(want) {
		t.Fatalf("got %d migrations, want %d: %+v", len(migrations), len(want), migrations)
	}
	for i, name := range want {
		if migrations[i].Name != name {
			t.Errorf("migration[%d] = %q, want %q", i, migrations[i].Name, name)
		}
	}
}

func TestLoadStripsPsqlMetaCommands(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"02-schemas-enums.sql": "\\connect melzone\nCREATE SCHEMA IF NOT EXISTS seg;\n  \\c melzone\nCREATE SCHEMA IF NOT EXISTS core;",
	})

	migrations, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(migrations) != 1 {
		t.Fatalf("got %d migrations, want 1", len(migrations))
	}

	sql := migrations[0].SQL
	if strings.Contains(sql, "\\") {
		t.Errorf("meta-commands survived: %q", sql)
	}
	if !strings.Contains(sql, "CREATE SCHEMA IF NOT EXISTS seg;") {
		t.Errorf("statement lost: %q", sql)
	}
	if !strings.Contains(sql, "CREATE SCHEMA IF NOT EXISTS core;") {
		t.Errorf("second statement lost: %q", sql)
	}
}

func TestLoadRejectsMissingDirectory(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("Load accepted a missing directory")
	}
}

func TestRunAppliesOnlyPendingFilesInOrder(t *testing.T) {
	files := map[string]string{
		"03-seg-tables.sql":   "CREATE TABLE seg.tb_user ();",
		"04-core-tables.sql":  "CREATE TABLE core.tb_place ();",
		"05-foreign-keys.sql": "ALTER TABLE core.tb_place ADD CONSTRAINT y FOREIGN KEY (co_user) REFERENCES seg.tb_user(co_user);",
	}

	executor := &fakeExecutor{}
	ledger := newFakeLedger("03-seg-tables.sql")
	runner := &Runner{Dir: t.TempDir(), Executor: executor, Ledger: ledger}
	writeFiles(t, runner.Dir, files)

	applied, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(applied) != 2 {
		t.Fatalf("applied = %v, want 2 pending files", applied)
	}
	if applied[0] != "04-core-tables.sql" || applied[1] != "05-foreign-keys.sql" {
		t.Errorf("applied = %v, want 04 then 05", applied)
	}
	if len(executor.ran) != 2 {
		t.Fatalf("executed %d statements, want 2", len(executor.ran))
	}
	if strings.Contains(executor.ran[0], "seg.tb_user") {
		t.Error("already-applied file was executed again")
	}
	if ledger.ensured != 1 {
		t.Errorf("Ensure called %d times, want 1", ledger.ensured)
	}
	if len(ledger.recorded) != 2 {
		t.Errorf("recorded %v, want 2 entries", ledger.recorded)
	}
}

func TestRunStopsAtFirstFailureAndDoesNotRecordIt(t *testing.T) {
	executor := &fakeExecutor{fail: map[string]error{"core": errors.New("syntax error")}}
	ledger := newFakeLedger()
	runner := &Runner{Dir: t.TempDir(), Executor: executor, Ledger: ledger}
	writeFiles(t, runner.Dir, map[string]string{
		"04-core-tables.sql":  "CREATE TABLE core.tb_place ();",
		"05-foreign-keys.sql": "ALTER TABLE core.tb_place ADD CONSTRAINT y FOREIGN KEY (co_user) REFERENCES seg.tb_user(co_user);",
	})

	_, err := runner.Run(context.Background())
	if err == nil {
		t.Fatal("Run succeeded, want error")
	}
	if !strings.Contains(err.Error(), "04-core-tables.sql") {
		t.Errorf("error %q does not name the failing file", err)
	}
	if len(executor.ran) != 1 {
		t.Errorf("executed %d files, want it to stop after the first failure", len(executor.ran))
	}
	if len(ledger.recorded) != 0 {
		t.Errorf("recorded %v, want nothing recorded for a failed file", ledger.recorded)
	}
}

func TestRunIsNoOpWhenEverythingIsApplied(t *testing.T) {
	executor := &fakeExecutor{}
	ledger := newFakeLedger("04-core-tables.sql")
	runner := &Runner{Dir: t.TempDir(), Executor: executor, Ledger: ledger}
	writeFiles(t, runner.Dir, map[string]string{
		"04-core-tables.sql": "CREATE TABLE core.tb_place ();",
	})

	applied, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(applied) != 0 {
		t.Errorf("applied = %v, want none", applied)
	}
	if len(executor.ran) != 0 {
		t.Errorf("executed %v, want nothing", executor.ran)
	}
}

func TestRunFailsWhenLedgerCannotBeEnsured(t *testing.T) {
	ledger := newFakeLedger()
	ledger.failEnsure = errors.New("permission denied")
	runner := &Runner{Dir: t.TempDir(), Executor: &fakeExecutor{}, Ledger: ledger}
	writeFiles(t, runner.Dir, map[string]string{"04-core-tables.sql": "CREATE TABLE core.tb_place ();"})

	if _, err := runner.Run(context.Background()); err == nil {
		t.Fatal("Run succeeded without a ledger, want error")
	}
}

func TestRetryGivesUpAfterAttempts(t *testing.T) {
	var calls int
	err := retry(context.Background(), 3, time.Millisecond, func(context.Context) error {
		calls++
		return errors.New("still down")
	})

	if err == nil {
		t.Fatal("retry succeeded, want error")
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestRetryStopsWhenOperationSucceeds(t *testing.T) {
	var calls int
	err := retry(context.Background(), 5, time.Millisecond, func(context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("not yet")
		}
		return nil
	})

	if err != nil {
		t.Fatalf("retry returned error: %v", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestRetryStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := retry(ctx, 5, time.Millisecond, func(context.Context) error {
		return errors.New("down")
	})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}
