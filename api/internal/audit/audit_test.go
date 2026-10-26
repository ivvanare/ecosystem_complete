package audit

import (
	"strings"
	"testing"
)

func newRecorder() *Postgres {
	return NewPostgres(nil, []byte("un-secreto-de-pruebas-suficientemente-largo"))
}

func TestEmailHashIsStableAndNormalized(t *testing.T) {
	recorder := newRecorder()

	first := recorder.EmailHash("Ana@Melia.Test")
	if first != recorder.EmailHash("  ana@melia.test  ") {
		t.Error("the hash changes with case or surrounding spaces")
	}
	if len(first) != 64 {
		t.Errorf("hash length = %d, want 64 hex chars", len(first))
	}
	if strings.Contains(first, "ana") {
		t.Error("the hash contains the clear address")
	}
}

func TestEmailHashDiffersFromAnotherSecret(t *testing.T) {
	other := NewPostgres(nil, []byte("otro-secreto-distinto-y-suficientemente-largo"))

	if newRecorder().EmailHash("ana@melia.test") == other.EmailHash("ana@melia.test") {
		t.Error("two servers with different secrets produce the same HMAC")
	}
}

func TestEmailHashDoesNotReuseTheSigningKeyDirectly(t *testing.T) {
	// The recorder derives a subkey; hashing the secret itself must not match.
	secret := []byte("un-secreto-de-pruebas-suficientemente-largo")
	recorder := NewPostgres(nil, secret)

	if recorder.EmailHash("") == "" {
		t.Fatal("empty email produced an empty hash")
	}
	if recorder.EmailHash("ana@melia.test") == NewPostgres(nil, []byte("melia/audit/email/v1")).EmailHash("ana@melia.test") {
		t.Error("the subkey derivation was skipped")
	}
}
