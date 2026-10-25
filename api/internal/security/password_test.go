package security

import (
	"regexp"
	"strings"
	"testing"
)

func TestHashPasswordRoundTrips(t *testing.T) {
	encoded, err := HashPassword("una-clave-larga-123")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if strings.Contains(encoded, "una-clave-larga-123") {
		t.Fatal("the encoded hash contains the clear password")
	}
	if !strings.HasPrefix(encoded, "argon2id$v=19$m=") {
		t.Errorf("encoded hash is not self-describing: %q", encoded)
	}

	ok, err := VerifyPassword(encoded, "una-clave-larga-123")
	if err != nil {
		t.Fatalf("VerifyPassword returned error: %v", err)
	}
	if !ok {
		t.Error("VerifyPassword rejected the correct password")
	}
}

func TestHashPasswordIsSalted(t *testing.T) {
	first, err := HashPassword("misma-clave-123456")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	second, err := HashPassword("misma-clave-123456")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if first == second {
		t.Error("two hashes of the same password are identical: the salt is not random")
	}
}

func TestVerifyPasswordRejectsWrongPassword(t *testing.T) {
	encoded, err := HashPassword("clave-correcta-123")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}

	ok, err := VerifyPassword(encoded, "clave-incorrecta-123")
	if err != nil {
		t.Fatalf("VerifyPassword returned error: %v", err)
	}
	if ok {
		t.Error("VerifyPassword accepted a wrong password")
	}
}

func TestVerifyPasswordRejectsMalformedHash(t *testing.T) {
	for _, encoded := range []string{"", "bcrypt$whatever", "argon2id$v=19$m=1", "argon2id$v=19$m=1,t=1,p=1$!!!$!!!"} {
		if _, err := VerifyPassword(encoded, "x"); err == nil {
			t.Errorf("VerifyPassword accepted malformed hash %q", encoded)
		}
	}
}

func TestGenerateCodeIsSixDigits(t *testing.T) {
	pattern := regexp.MustCompile(`^[0-9]{6}$`)

	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		code, err := GenerateCode()
		if err != nil {
			t.Fatalf("GenerateCode returned error: %v", err)
		}
		if !pattern.MatchString(code) {
			t.Fatalf("code %q is not six digits", code)
		}
		seen[code] = true
	}

	if len(seen) < 100 {
		t.Errorf("only %d distinct codes in 200 draws: entropy looks wrong", len(seen))
	}
}

func TestHashCodeNeverReturnsTheCode(t *testing.T) {
	code := "042317"
	hash := HashCode(code)

	if strings.Contains(hash, code) {
		t.Fatal("HashCode embeds the clear code")
	}
	if len(hash) != 64 {
		t.Errorf("hash length = %d, want 64 hex chars (sha256)", len(hash))
	}
	if HashCode(code) != hash {
		t.Error("HashCode is not deterministic")
	}
	if HashCode("042318") == hash {
		t.Error("HashCode collided for different codes")
	}
}

func TestCodesEqual(t *testing.T) {
	if !CodesEqual("123456", "123456") {
		t.Error("CodesEqual rejected identical codes")
	}
	if CodesEqual("123456", "123457") {
		t.Error("CodesEqual accepted different codes")
	}
}
