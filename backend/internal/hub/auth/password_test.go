package auth

import (
	"strings"
	"testing"
	"time"
)

func TestHashAndCheckPassword(t *testing.T) {
	password := "securepassword123"
	hash, err := HashPassword(password)

	if err != nil {
		t.Fatalf("HashPassword() error: %v", err)
	}

	if hash == "" {
		t.Fatal("HashPassword() returned empty hash")
	}

	if hash == password {
		t.Error("hash should not equal plaintext password")
	}

	if !CheckPassword(password, hash) {
		t.Error("CheckPassword() returned false for correct password")
	}

}

func TestHashPassword_EmptyPassword(t *testing.T) {
	_, err := HashPassword("")
	if err == nil {
		t.Fatal("HashPassword(\"\") expected error for empty password")
	}
}

func TestCheckPassword_InvalidHash(t *testing.T) {
	if CheckPassword("password", "not-a-valid-hash") {
		t.Error("CheckPassword() returned true for invalid hash string")
	}
}

func TestHashPassword_MultipleHashes(t *testing.T) {
	pw := "repeatable"
	hash1, err1 := HashPassword(pw)
	hash2, err2 := HashPassword(pw)
	if err1 != nil || err2 != nil {
		t.Fatalf("HashPassword() error: %v, %v", err1, err2)
	}
	if hash1 == hash2 {
		t.Error("HashPassword() produced same hash for same password twice (should be different due to salt)")
	}
}

func TestHashPassword_LongPassword(t *testing.T) {
	longPw := make([]byte, 1000)
	for i := range longPw {
		longPw[i] = 'a'
	}
	hash, err := HashPassword(string(longPw))
	if err != nil {
		t.Fatalf("HashPassword(long) error: %v", err)
	}
	if !CheckPassword(string(longPw), hash) {
		t.Error("CheckPassword() failed for long password")
	}
}

func TestHashPassword_UnicodePassword(t *testing.T) {
	pw := "pässwörd-测试-パスワード" // #nosec
	hash, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword(unicode) error: %v", err)
	}
	if !CheckPassword(pw, hash) {
		t.Error("CheckPassword() failed for unicode password")
	}
}

func TestCheckPassword_WrongPassword(t *testing.T) {
	hash, err := HashPassword("correcthorsebatterystaple")
	if err != nil {
		t.Fatalf("HashPassword() error: %v", err)
	}
	if CheckPassword("wrongpassword", hash) {
		t.Error("CheckPassword() returned true for incorrect password")
	}
}

func TestCheckPassword_EmptyPassword(t *testing.T) {
	if CheckPassword("", "somehash") {
		t.Error("CheckPassword() returned true for empty password")
	}
}

func TestCheckPassword_EmptyHash(t *testing.T) {
	if CheckPassword("password", "") {
		t.Error("CheckPassword() returned true for empty hash")
	}
}

func TestInitPassword_SetsDummyHash(t *testing.T) {
	prev := dummyHash
	dummyHash = ""
	t.Cleanup(func() { dummyHash = prev })

	if err := initPassword(); err != nil {
		t.Fatalf("initPassword() error: %v", err)
	}
	if dummyHash == "" {
		t.Fatal("initPassword() did not set dummyHash")
	}
	if !strings.HasPrefix(dummyHash, "$argon2id$") {
		t.Errorf("dummyHash does not look like an argon2id hash: %q", dummyHash)
	}
}

func TestCompareWithDummy_DoesNotPanic(t *testing.T) {
	if err := initPassword(); err != nil {
		t.Fatalf("initPassword() error: %v", err)
	}

	CompareWithDummy("some-password")
	CompareWithDummy("")
	CompareWithDummy(strings.Repeat("a", 1000))
}

func TestValidatePasswordStrength(t *testing.T) {
	tests := []struct {
		password string
		want     bool
	}{
		{"short", false},
		{"alllowercase123!", false},
		{"ALLUPPERCASE123!", false},
		{"NoNumbers!NoNumbers!", false},
		{"NoSpecial1234ABCDEF", false},
		{"ValidPass1!ValidPass", true},
		{"Abcdefghijk1!", true},
		// Non-ASCII: Ä counts as uppercase, ß as lowercase, @ as special.
		{"Äbcdefghijk1@", true},
		// Non-ASCII-only upper/lower still satisfies the policy.
		{"Äßöüàé1234!!", true},
	}
	for _, tc := range tests {
		if got := ValidatePasswordStrength(tc.password); got != tc.want {
			t.Errorf("ValidatePasswordStrength(%q) = %v, want %v", tc.password, got, tc.want)
		}
	}
}

func TestGenerateRandomPassword_MeetsStrengthPolicy(t *testing.T) {
	for range 20 {
		pw, err := GenerateRandomPassword()
		if err != nil {
			t.Fatalf("GenerateRandomPassword() error: %v", err)
		}
		if !ValidatePasswordStrength(pw) {
			t.Errorf("generated password failed strength check: %q", pw)
		}
	}
}

func TestCompareWithDummy_Timing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timing test in short mode")
	}

	if err := initPassword(); err != nil {
		t.Fatalf("initPassword() error: %v", err)
	}

	hash, err := HashPassword("reference-password")
	if err != nil {
		t.Fatalf("HashPassword() error: %v", err)
	}

	const runs = 3
	var realTotal, dummyTotal time.Duration
	for range runs {
		start := time.Now()
		CheckPassword("wrong-password", hash)
		realTotal += time.Since(start)

		start = time.Now()
		CompareWithDummy("wrong-password")
		dummyTotal += time.Since(start)
	}

	realAvg := realTotal / runs
	dummyAvg := dummyTotal / runs

	diff := realAvg - dummyAvg
	if diff < 0 {
		diff = -diff
	}
	threshold := realAvg / 2
	if diff > threshold {
		t.Errorf("timing difference too large: real=%v dummy=%v diff=%v (threshold=%v)",
			realAvg, dummyAvg, diff, threshold)
	}
}
