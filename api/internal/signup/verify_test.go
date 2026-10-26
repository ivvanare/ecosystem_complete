package signup

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ivvanare/ecosystem_complete/api/internal/security"
	"github.com/ivvanare/ecosystem_complete/api/internal/session"
)

type fakeTx struct {
	pending     *PendingRow
	pendingErr  error
	attempts    int
	promoted    *NewUser
	userID      string
	commits     int
	rollbacks   int
	failPromote error
}

func (f *fakeTx) Pending(context.Context) (PendingRow, error) {
	if f.pendingErr != nil {
		return PendingRow{}, f.pendingErr
	}
	if f.pending == nil {
		return PendingRow{}, ErrNoPending
	}
	return *f.pending, nil
}

func (f *fakeTx) RecordFailedAttempt(context.Context) error {
	f.attempts++
	return nil
}

func (f *fakeTx) Promote(_ context.Context, user NewUser) (string, error) {
	if f.failPromote != nil {
		return "", f.failPromote
	}
	f.promoted = &user
	if f.userID == "" {
		f.userID = "user-1"
	}
	return f.userID, nil
}

func (f *fakeTx) DiscardPending(context.Context) error {
	f.pending = nil
	return nil
}

func (f *fakeTx) Commit(context.Context) error { f.commits++; return nil }

func (f *fakeTx) Rollback(context.Context) error { f.rollbacks++; return nil }

type fakeVerifierStore struct {
	tx  *fakeTx
	err error
}

func (f *fakeVerifierStore) BeginVerify(context.Context, string) (Tx, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.tx, nil
}

type fakeIssuer struct {
	calls int
	user  string
	err   error
}

func (f *fakeIssuer) Issue(_ context.Context, userID string, _ session.Meta) (session.Tokens, error) {
	f.calls++
	f.user = userID
	if f.err != nil {
		return session.Tokens{}, f.err
	}
	return session.Tokens{AccessToken: "access", RefreshToken: "refresh"}, nil
}

var verifyNow = time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)

func pendingRow(code string, attempts int, expires time.Time) *PendingRow {
	return &PendingRow{
		Name:         "Ana",
		PasswordHash: "argon2id$v=19$m=65536,t=1,p=4$abc$def",
		CodeHash:     security.HashCode(code),
		Attempts:     attempts,
		ExpiresAt:    expires,
	}
}

func newVerifier(store VerifierStore, issuer TokenIssuer) *Verifier {
	verifier := NewVerifier(store, issuer)
	verifier.Now = func() time.Time { return verifyNow }
	return verifier
}

func TestVerifyPromotesTheUserAndIssuesTokens(t *testing.T) {
	// Keep a handle on the row: DiscardPending nils the transaction's copy.
	row := pendingRow("042317", 0, verifyNow.Add(5*time.Minute))
	tx := &fakeTx{pending: row}
	issuer := &fakeIssuer{}
	verifier := newVerifier(&fakeVerifierStore{tx: tx}, issuer)

	tokens, err := verifier.Verify(context.Background(), "  ANA@melia.test ", "042317", session.Meta{Device: "iPhone"})
	if err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}

	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Errorf("tokens = %+v, want a pair", tokens)
	}
	if tx.promoted == nil {
		t.Fatal("the user was not created")
	}
	if tx.promoted.Email != "ana@melia.test" {
		t.Errorf("created user email = %q, want normalized", tx.promoted.Email)
	}
	if tx.promoted.Name != "Ana" {
		t.Errorf("created user name = %q", tx.promoted.Name)
	}
	if !tx.promoted.EmailVerifiedAt.Equal(verifyNow) {
		t.Errorf("EmailVerifiedAt = %v, want now: the code proved ownership", tx.promoted.EmailVerifiedAt)
	}
	if tx.promoted.PasswordHash != row.PasswordHash {
		t.Error("the stored password hash was not carried over")
	}
	if tx.commits != 1 || tx.rollbacks != 0 {
		t.Errorf("commits=%d rollbacks=%d, want 1/0", tx.commits, tx.rollbacks)
	}
	if issuer.calls != 1 || issuer.user != "user-1" {
		t.Errorf("issuer called %d times for %q", issuer.calls, issuer.user)
	}
}

func TestVerifyWithAWrongCodeCountsTheAttemptAndCommits(t *testing.T) {
	tx := &fakeTx{pending: pendingRow("042317", 0, verifyNow.Add(5*time.Minute))}
	issuer := &fakeIssuer{}
	verifier := newVerifier(&fakeVerifierStore{tx: tx}, issuer)

	_, err := verifier.Verify(context.Background(), "ana@melia.test", "999999", session.Meta{})
	if !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("error = %v, want ErrInvalidCode", err)
	}
	if tx.attempts != 1 {
		t.Errorf("attempts = %d, want 1", tx.attempts)
	}
	if tx.commits != 1 {
		t.Error("the incremented counter was not committed: the attempt would be free")
	}
	if tx.promoted != nil {
		t.Error("a user was created with a wrong code")
	}
	if issuer.calls != 0 {
		t.Error("tokens were issued with a wrong code")
	}
}

func TestVerifyRejectsASpentCounter(t *testing.T) {
	tx := &fakeTx{pending: pendingRow("042317", MaxAttempts, verifyNow.Add(5*time.Minute))}
	verifier := newVerifier(&fakeVerifierStore{tx: tx}, &fakeIssuer{})

	if _, err := verifier.Verify(context.Background(), "ana@melia.test", "042317", session.Meta{}); !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("error = %v, want ErrTooManyAttempts", err)
	}
	if tx.promoted != nil {
		t.Error("a user was created after the counter was spent")
	}
}

func TestVerifyRejectsAnExpiredCode(t *testing.T) {
	tx := &fakeTx{pending: pendingRow("042317", 0, verifyNow.Add(-time.Second))}
	verifier := newVerifier(&fakeVerifierStore{tx: tx}, &fakeIssuer{})

	if _, err := verifier.Verify(context.Background(), "ana@melia.test", "042317", session.Meta{}); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("error = %v, want ErrInvalidCode", err)
	}
	if tx.promoted != nil {
		t.Error("an expired code created a user")
	}
}

func TestVerifyWithoutAPendingRegistrationIsIndistinguishableFromAWrongCode(t *testing.T) {
	tx := &fakeTx{}
	verifier := newVerifier(&fakeVerifierStore{tx: tx}, &fakeIssuer{})

	if _, err := verifier.Verify(context.Background(), "nadie@melia.test", "042317", session.Meta{}); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("error = %v, want ErrInvalidCode", err)
	}
}

func TestVerifyRollsBackWhenPromotionFails(t *testing.T) {
	tx := &fakeTx{
		pending:     pendingRow("042317", 0, verifyNow.Add(5*time.Minute)),
		failPromote: errors.New("unique violation"),
	}
	verifier := newVerifier(&fakeVerifierStore{tx: tx}, &fakeIssuer{})

	if _, err := verifier.Verify(context.Background(), "ana@melia.test", "042317", session.Meta{}); err == nil {
		t.Fatal("Verify succeeded, want error")
	}
	if tx.commits != 0 {
		t.Error("a failed promotion was committed")
	}
	if tx.rollbacks != 1 {
		t.Errorf("rollbacks = %d, want 1", tx.rollbacks)
	}
}
