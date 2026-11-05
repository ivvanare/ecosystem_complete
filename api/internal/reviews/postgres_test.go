package reviews

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// The SQL shape is asserted against a fake that records the statements, because these
// tests must not need PostgreSQL (p1-discovery.md §8, AGENTS.md). The live-database
// proof of the same statements runs separately, inside a transaction that rolls back.

type recordedQuery struct {
	sql  string
	args []any
}

// fakeTx records the write path. rowQueue feeds the QueryRow calls in order (the write
// RETURNING first, then the author-name read).
type fakeTx struct {
	recorded   []recordedQuery
	execTag    pgconn.CommandTag
	execErr    error
	rowQueue   []*fakeRow
	committed  bool
	rolledBack bool
}

func (t *fakeTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.recorded = append(t.recorded, recordedQuery{sql, args})
	return t.execTag, t.execErr
}

func (t *fakeTx) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	t.recorded = append(t.recorded, recordedQuery{sql, args})
	return &fakeRows{}, nil
}

func (t *fakeTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	t.recorded = append(t.recorded, recordedQuery{sql, args})
	if len(t.rowQueue) == 0 {
		return &fakeRow{err: fmt.Errorf("unexpected QueryRow: %s", sql)}
	}
	row := t.rowQueue[0]
	t.rowQueue = t.rowQueue[1:]
	return row
}

func (t *fakeTx) Commit(context.Context) error {
	t.committed = true
	return nil
}

func (t *fakeTx) Rollback(context.Context) error {
	t.rolledBack = true
	return nil
}

// fakeDB records the read path and hands the write path a scripted transaction.
type fakeDB struct {
	recorded []recordedQuery
	tx       *fakeTx
	exists   bool
	count    int
	rows     [][]any
	rowErr   error
}

func (f *fakeDB) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	f.recorded = append(f.recorded, recordedQuery{sql, args})
	return &fakeRows{data: f.rows}, nil
}

func (f *fakeDB) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	f.recorded = append(f.recorded, recordedQuery{sql, args})
	switch {
	case strings.Contains(sql, "EXISTS"):
		return &fakeRow{values: []any{f.exists}, err: f.rowErr}
	case strings.Contains(sql, "COUNT(*)"):
		return &fakeRow{values: []any{f.count}, err: f.rowErr}
	case strings.Contains(sql, "JOIN seg.tb_user"):
		if len(f.rows) == 0 {
			return &fakeRow{err: pgx.ErrNoRows}
		}
		return &fakeRow{values: f.rows[0], err: f.rowErr}
	}
	return &fakeRow{err: fmt.Errorf("unexpected QueryRow: %s", sql)}
}

func (f *fakeDB) Begin(context.Context) (transaction, error) {
	if f.tx == nil {
		f.tx = &fakeTx{}
	}
	return f.tx, nil
}

type fakeRow struct {
	values []any
	err    error
}

func (r *fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return fmt.Errorf("scan %d dest for %d values", len(dest), len(r.values))
	}
	for i := range dest {
		target := reflect.ValueOf(dest[i])
		if target.Kind() != reflect.Pointer {
			return fmt.Errorf("scan dest %d is not a pointer", i)
		}
		if r.values[i] == nil {
			target.Elem().Set(reflect.Zero(target.Elem().Type()))
			continue
		}
		value := reflect.ValueOf(r.values[i])
		if target.Elem().Kind() == reflect.Pointer {
			allocated := reflect.New(target.Elem().Type().Elem())
			allocated.Elem().Set(value)
			target.Elem().Set(allocated)
			continue
		}
		if !value.Type().AssignableTo(target.Elem().Type()) {
			return fmt.Errorf("cannot assign %T to %s", r.values[i], target.Elem().Type())
		}
		target.Elem().Set(value)
	}
	return nil
}

type fakeRows struct {
	data [][]any
	idx  int
	err  error
}

func (r *fakeRows) Close()                                       {}
func (r *fakeRows) TypeMap() *pgtype.Map                         { return nil }
func (r *fakeRows) Err() error                                   { return r.err }
func (r *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRows) RawValues() [][]byte                          { return nil }
func (r *fakeRows) Conn() *pgx.Conn                              { return nil }
func (r *fakeRows) Values() ([]any, error)                       { return r.data[r.idx-1], nil }

func (r *fakeRows) Next() bool {
	if r.idx >= len(r.data) {
		return false
	}
	r.idx++
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	return (&fakeRow{values: r.data[r.idx-1]}).Scan(dest...)
}

// reviewValues are the eight RETURNING columns of a write, in scan order.
func reviewValues() []any {
	now := time.Now()
	return []any{"r1", "p1", "u1", 5, "la mejor", StatusActive, now, now}
}

func newTestStore(db *fakeDB) *PostgresStore {
	return &PostgresStore{db: db}
}

// §8 item 20 / S13: the transaction announces the actor before the insert, so the
// history trigger records co_actor_user. The service never writes the history table.
func TestCreateSetsTheActorBeforeInserting(t *testing.T) {
	db := &fakeDB{tx: &fakeTx{rowQueue: []*fakeRow{
		{values: reviewValues()},
		{values: []any{"Ana"}},
	}}}
	comment := "la mejor"
	review, err := newTestStore(db).Create(context.Background(), "u1", "p1", CreateInput{Rating: 5, Comment: &comment})
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	if len(db.tx.recorded) != 3 {
		t.Fatalf("recorded %d statements, want 3 (set_config, insert, author): %v", len(db.tx.recorded), db.tx.recorded)
	}
	first := db.tx.recorded[0]
	if !strings.Contains(first.sql, "set_config('app.current_user_id', $1, true)") {
		t.Errorf("first statement = %q, want the actor set_config", first.sql)
	}
	if len(first.args) != 1 || first.args[0] != "u1" {
		t.Errorf("set_config args = %v, want [u1]", first.args)
	}

	insert := db.tx.recorded[1]
	if !strings.Contains(insert.sql, "INSERT INTO core.tb_review") {
		t.Errorf("second statement = %q, want the insert", insert.sql)
	}
	if len(insert.args) != 4 || insert.args[0] != "u1" || insert.args[1] != "p1" || insert.args[2] != 5 {
		t.Errorf("insert args = %v", insert.args)
	}
	for _, stmt := range db.tx.recorded {
		if strings.Contains(stmt.sql, "tb_review_history") {
			t.Errorf("the service must never write the history table: %q", stmt.sql)
		}
	}
	if !db.tx.committed || db.tx.rolledBack {
		t.Errorf("committed=%v rolledBack=%v", db.tx.committed, db.tx.rolledBack)
	}

	if review.ID != "r1" || review.AuthorID != "u1" || review.Rating != 5 {
		t.Errorf("review = %+v", review)
	}
	if review.AuthorName == nil || *review.AuthorName != "Ana" {
		t.Errorf("author name = %v", review.AuthorName)
	}
}

// §8 item 15 / S10: SQLSTATE 23505 on the partial unique index maps to
// ErrAlreadyExists — the 409 — never a pre-check and never a 500.
func TestCreateMapsThePartialUniqueToAlreadyExists(t *testing.T) {
	db := &fakeDB{tx: &fakeTx{rowQueue: []*fakeRow{
		{err: &pgconn.PgError{Code: "23505", ConstraintName: "ux_tb_review_user_place_active"}},
	}}}
	_, err := newTestStore(db).Create(context.Background(), "u1", "p1", CreateInput{Rating: 5})
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("err = %v, want ErrAlreadyExists", err)
	}
	if !db.tx.rolledBack || db.tx.committed {
		t.Errorf("a rejected write must roll back: committed=%v rolledBack=%v", db.tx.committed, db.tx.rolledBack)
	}
}

// Only that constraint maps: another 23505 is an ordinary failure, not a conflict.
func TestCreateDoesNotMapAnotherConstraint(t *testing.T) {
	db := &fakeDB{tx: &fakeTx{rowQueue: []*fakeRow{
		{err: &pgconn.PgError{Code: "23505", ConstraintName: "some_other_index"}},
	}}}
	_, err := newTestStore(db).Create(context.Background(), "u1", "p1", CreateInput{Rating: 5})
	if errors.Is(err, ErrAlreadyExists) {
		t.Fatal("a foreign constraint must not map to ErrAlreadyExists")
	}
	if err == nil {
		t.Fatal("a 23505 is still an error")
	}
}

// The edit touches only what was sent (COALESCE), stays scoped to live rows, announces
// the actor, and bumps dt_updated_at.
func TestUpdateSetsTheActorAndKeepsLiveRows(t *testing.T) {
	db := &fakeDB{tx: &fakeTx{rowQueue: []*fakeRow{
		{values: reviewValues()},
		{values: []any{"Ana"}},
	}}}
	rating := 4
	_, err := newTestStore(db).Update(context.Background(), "u1", "r1", UpdateInput{Rating: &rating})
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	if len(db.tx.recorded) != 3 {
		t.Fatalf("recorded %d statements, want 3: %v", len(db.tx.recorded), db.tx.recorded)
	}
	if !strings.Contains(db.tx.recorded[0].sql, "set_config('app.current_user_id', $1, true)") {
		t.Errorf("first statement = %q, want the actor set_config", db.tx.recorded[0].sql)
	}
	update := db.tx.recorded[1].sql
	for _, want := range []string{"UPDATE core.tb_review", "COALESCE", "dt_deleted_at IS NULL", "dt_updated_at = now()"} {
		if !strings.Contains(update, want) {
			t.Errorf("update missing %q: %s", want, update)
		}
	}
}

// §8 item 16 / S11: the delete is logical — an UPDATE of dt_deleted_at, never a
// DELETE FROM — so the partial index frees the pair and the author may review again.
func TestDeleteIsLogicalAndSetsTheActor(t *testing.T) {
	db := &fakeDB{tx: &fakeTx{execTag: pgconn.NewCommandTag("UPDATE 1")}}
	if err := newTestStore(db).Delete(context.Background(), "u1", "r1"); err != nil {
		t.Fatalf("err = %v", err)
	}

	if len(db.tx.recorded) != 2 {
		t.Fatalf("recorded %d statements, want 2: %v", len(db.tx.recorded), db.tx.recorded)
	}
	if !strings.Contains(db.tx.recorded[0].sql, "set_config('app.current_user_id', $1, true)") {
		t.Errorf("first statement = %q, want the actor set_config", db.tx.recorded[0].sql)
	}
	delete := db.tx.recorded[1].sql
	if strings.Contains(delete, "DELETE FROM") {
		t.Errorf("the delete must be logical, never physical: %s", delete)
	}
	for _, want := range []string{"UPDATE core.tb_review", "SET dt_deleted_at = now()", "dt_deleted_at IS NULL"} {
		if !strings.Contains(delete, want) {
			t.Errorf("delete missing %q: %s", want, delete)
		}
	}
	if !db.tx.committed {
		t.Error("the logical delete was not committed")
	}
}

func TestDeleteMissingReviewIsNotFound(t *testing.T) {
	db := &fakeDB{tx: &fakeTx{execTag: pgconn.NewCommandTag("UPDATE 0")}}
	err := newTestStore(db).Delete(context.Background(), "u1", "r1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// S12: moderation reuses the existing review_status enum and announces the actor.
func TestModerateSetsStatusAndActor(t *testing.T) {
	db := &fakeDB{tx: &fakeTx{rowQueue: []*fakeRow{
		{values: reviewValues()},
		{values: []any{"Ana"}},
	}}}
	if _, err := newTestStore(db).Moderate(context.Background(), "admin-1", "r1", StatusModerated); err != nil {
		t.Fatalf("err = %v", err)
	}

	if len(db.tx.recorded) != 3 {
		t.Fatalf("recorded %d statements, want 3: %v", len(db.tx.recorded), db.tx.recorded)
	}
	first := db.tx.recorded[0]
	if !strings.Contains(first.sql, "set_config('app.current_user_id', $1, true)") || first.args[0] != "admin-1" {
		t.Errorf("first statement = %q args %v, want the moderator set_config", first.sql, first.args)
	}
	moderate := db.tx.recorded[1]
	if !strings.Contains(moderate.sql, "tx_status = $2::public.review_status") {
		t.Errorf("moderation must reuse the existing enum: %s", moderate.sql)
	}
	if len(moderate.args) != 2 || moderate.args[1] != StatusModerated {
		t.Errorf("moderate args = %v", moderate.args)
	}
}

// §8 item 18 / S11: the list predicate hides non-ACTIVE or deleted rows from everyone
// but the author ($2) and moderators ($3), and S14: the read never aggregates for
// ordering — newest first, no AVG.
func TestListVisibilityPredicateAndNoAggregation(t *testing.T) {
	now := time.Now()
	db := &fakeDB{
		count: 1,
		rows:  [][]any{{"r1", "p1", "u1", "Ana", 5, "la mejor", StatusActive, now, now}},
	}
	rows, total, err := newTestStore(db).List(context.Background(), "p1", Viewer{UserID: "u1", Moderator: true}, 2, 20)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if total != 1 || len(rows) != 1 {
		t.Fatalf("total=%d rows=%d", total, len(rows))
	}
	if rows[0].ID != "r1" || rows[0].AuthorName == nil || *rows[0].AuthorName != "Ana" {
		t.Errorf("row = %+v", rows[0])
	}

	if len(db.recorded) != 2 {
		t.Fatalf("recorded %d queries, want 2 (count, page): %v", len(db.recorded), db.recorded)
	}
	count, page := db.recorded[0], db.recorded[1]
	if !strings.Contains(count.sql, "COUNT(*)") {
		t.Errorf("first query = %q, want the count", count.sql)
	}
	for _, want := range []string{"r.tx_status = 'ACTIVE'", "$2::uuid", "$3::boolean"} {
		if !strings.Contains(count.sql, want) || !strings.Contains(page.sql, want) {
			t.Errorf("visibility predicate missing %q\ncount: %s\npage: %s", want, count.sql, page.sql)
		}
	}
	if !strings.Contains(page.sql, "ORDER BY r.dt_created_at DESC") {
		t.Errorf("the page is newest-first: %s", page.sql)
	}
	if strings.Contains(page.sql, "AVG(") || strings.Contains(page.sql, "SUM(") {
		t.Errorf("the read path must never aggregate reviews: %s", page.sql)
	}
	// Viewer, moderator flag and paging travel as bound parameters.
	if len(page.args) != 5 || page.args[1] != "u1" || page.args[2] != true || page.args[3] != 20 || page.args[4] != 20 {
		t.Errorf("page args = %v, want [p1 u1 true 20 20]", page.args)
	}
	// Anonymous: no author id, no moderator flag.
	anon := &fakeDB{}
	if _, _, err := newTestStore(anon).List(context.Background(), "p1", Viewer{}, 1, 20); err != nil {
		t.Fatalf("err = %v", err)
	}
	if anon.recorded[1].args[1] != nil || anon.recorded[1].args[2] != false {
		t.Errorf("anonymous args = %v, want nil author and false moderator", anon.recorded[1].args)
	}
}

// The author check reads one live review of the place: deleted rows are invisible.
func TestGetScopesToThePlaceAndLiveRows(t *testing.T) {
	now := time.Now()
	db := &fakeDB{rows: [][]any{{"r1", "p1", "u1", "Ana", 5, "ok", StatusActive, now, now}}}
	review, err := newTestStore(db).Get(context.Background(), "p1", "r1")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if review.AuthorID != "u1" {
		t.Errorf("review = %+v", review)
	}
	got := db.recorded[0].sql
	for _, want := range []string{"r.co_place = $1::uuid", "r.co_review = $2::uuid", "r.dt_deleted_at IS NULL"} {
		if !strings.Contains(got, want) {
			t.Errorf("get missing %q: %s", want, got)
		}
	}

	missing := &fakeDB{}
	if _, err := newTestStore(missing).Get(context.Background(), "p1", "r9"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound for a missing row", err)
	}
}

// S14: place liveness is deleted-aware and status-aware.
func TestPlaceActivePredicate(t *testing.T) {
	db := &fakeDB{exists: true}
	active, err := newTestStore(db).PlaceActive(context.Background(), "p1")
	if err != nil || !active {
		t.Fatalf("active=%v err=%v", active, err)
	}
	got := db.recorded[0].sql
	for _, want := range []string{"dt_deleted_at IS NULL", "tx_status = 'ACTIVE'"} {
		if !strings.Contains(got, want) {
			t.Errorf("place check missing %q: %s", want, got)
		}
	}
}
