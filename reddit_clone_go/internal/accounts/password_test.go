package accounts

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestPasswordBytesTruncation(t *testing.T) {
	for n, want := range map[int]int{0: 0, 71: 71, 72: 72, 73: 72, 200: 72} {
		if got := len(passwordBytes(strings.Repeat("a", n))); got != want {
			t.Errorf("passwordBytes(%d bytes) = %d bytes, want %d", n, got, want)
		}
	}
	// The cut is by bytes, not runes (30 three-byte runes = 90 bytes).
	if got := len(passwordBytes(strings.Repeat("あ", 30))); got != 72 {
		t.Errorf("multibyte truncation = %d bytes, want 72", got)
	}
}

// A hash produced by the Python stack (passlib + bcrypt) for a
// 104-byte password must verify here once the input is truncated the
// same way, and changing anything past byte 72 must not matter —
// exactly what the Python side does.
func TestLongPasswordAgainstPasslibHash(t *testing.T) {
	passlibHash := "$2b$12$M6qxEH/b1PqWXCczVJAIcOL6dJvBgCog3ZacCBe6Y//bhy5U6hkei"
	password := "long-password-" + strings.Repeat("x", 90) // 104 bytes

	if err := bcrypt.CompareHashAndPassword(
		[]byte(passlibHash), passwordBytes(password)); err != nil {
		t.Fatalf("passlib hash does not verify: %v", err)
	}

	changedTail := "long-password-" + strings.Repeat("x", 89) + "y"
	if err := bcrypt.CompareHashAndPassword(
		[]byte(passlibHash), passwordBytes(changedTail)); err != nil {
		t.Errorf("change past byte 72 should not matter: %v", err)
	}

	changedHead := "long-passwore-" + strings.Repeat("x", 90)
	if err := bcrypt.CompareHashAndPassword(
		[]byte(passlibHash), passwordBytes(changedHead)); err == nil {
		t.Error("change within the first 72 bytes must fail")
	}
}

// Generating a hash for an over-long password must not error (it did
// before the truncation, surfacing as a 500 on registration).
func TestLongPasswordHashGeneration(t *testing.T) {
	password := strings.Repeat("p", 200)
	hash, err := bcrypt.GenerateFromPassword(passwordBytes(password), bcryptCost)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword(hash, passwordBytes(password)); err != nil {
		t.Errorf("generated hash does not verify: %v", err)
	}
}
