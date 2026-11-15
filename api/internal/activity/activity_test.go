package activity

import (
	"context"
	"errors"
	"testing"
)

type fakeStore struct {
	snapshot Snapshot
	err      error
	calls    int
}

func (f *fakeStore) Snapshot(context.Context) (Snapshot, error) {
	f.calls++
	return f.snapshot, f.err
}

// A dashboard over an empty environment is a real first day: the answer must be empty arrays, not
// null, so a client can render it without guarding every field.
func TestAnEmptySnapshotAnswersEmptyArrays(t *testing.T) {
	snapshot, err := New(&fakeStore{}).Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snapshot.Windows == nil || snapshot.Recent == nil {
		t.Fatalf("snapshot = %+v, want empty slices", snapshot)
	}
}

func TestTheStoreAnswerIsPassedThrough(t *testing.T) {
	store := &fakeStore{snapshot: Snapshot{
		Windows: []Window{{Label: "5m", Signups: 2}},
		Totals:  Totals{Users: 7},
		Recent:  []Recent{{Kind: "signup"}},
	}}
	snapshot, err := New(store).Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if store.calls != 1 || snapshot.Windows[0].Signups != 2 || snapshot.Totals.Users != 7 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

// The handler maps a failure to a 500 with the generic envelope; the domain wraps it so the log
// says which projection failed.
func TestAFailingStoreIsAnError(t *testing.T) {
	_, err := New(&fakeStore{err: errors.New("db down")}).Snapshot(context.Background())
	if err == nil {
		t.Fatal("a failing store was reported as success")
	}
}
