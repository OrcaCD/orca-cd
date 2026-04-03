package auth

import "testing"

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
