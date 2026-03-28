package crypto

import (
	"strings"
	"testing"
)

// validKey is a 32-byte key expressed as 64 hex characters.
const validKey = "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"

func mustInit(t *testing.T) {
	t.Helper()
	if err := Init(validKey); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
}

func TestInit_ValidKey(t *testing.T) {
	if err := Init(validKey); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestEncryptDecrypt_Simple(t *testing.T) {
	mustInit(t)

	plaintext := "hello, world"
	ciphertext, err := Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	got, err := Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != plaintext {
		t.Errorf("round-trip mismatch: got %q, want %q", got, plaintext)
	}
}

func TestEncryptDecrypt_EmptyString(t *testing.T) {
	mustInit(t)

	ciphertext, err := Encrypt("")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestEncryptDecrypt_LongText(t *testing.T) {
	mustInit(t)

	plaintext := strings.Repeat("abcdefghij", 1000)
	ciphertext, err := Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != plaintext {
		t.Error("round-trip mismatch for long text")
	}
}

func TestEncryptDecrypt_EmojiString(t *testing.T) {
	mustInit(t)

	ciphertext, err := Encrypt("😊🚀🌟")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != "😊🚀🌟" {
		t.Errorf("expected %q, got %q", "😊🚀🌟", got)
	}
}

func TestEncrypt_ProducesUniqueOutputs(t *testing.T) {
	mustInit(t)

	plaintext := "same input"
	c1, err := Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt 1: %v", err)
	}
	c2, err := Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt 2: %v", err)
	}
	if c1 == c2 {
		t.Error("expected different ciphertexts for the same plaintext (random nonce)")
	}
}

func TestDecrypt_TamperedCiphertext(t *testing.T) {
	mustInit(t)

	ciphertext, err := Encrypt("sensitive data")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// Flip the first byte of the ciphertext.
	tampered := []byte(ciphertext)
	tampered[0] ^= 0xff
	if _, err := Decrypt(string(tampered)); err == nil {
		t.Error("expected error when decrypting tampered ciphertext")
	}
}

func TestDecrypt_TamperedNonce(t *testing.T) {
	mustInit(t)

	ciphertext, err := Encrypt("sensitive data")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// Replace the trailing nonce hex with zeros.
	noncePart := strings.Repeat("0", 64)
	body := ciphertext[:len(ciphertext)-64]
	if _, err := Decrypt(body + noncePart); err == nil {
		t.Error("expected error when decrypting with wrong nonce")
	}
}
