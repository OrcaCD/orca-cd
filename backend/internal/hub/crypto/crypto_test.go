package crypto

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/aegis-aead/go-libaegis/aegis256"
)

// validKey is an example application secret string used for tests. It is treated
// as an arbitrary string by Init, not as hex-encoded key material.
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

	// Decode, zero out the nonce bytes, then re-encode.
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	for i := range aegis256.NonceSize {
		data[i] = 0
	}
	tampered := base64.StdEncoding.EncodeToString(data)
	if _, err := Decrypt(tampered); err == nil {
		t.Error("expected error when decrypting with wrong nonce")
	}
}

func TestDecrypt_InvalidBase64(t *testing.T) {
	mustInit(t)

	if _, err := Decrypt("not-valid-base64!!!"); err == nil {
		t.Error("expected error for invalid base64 input")
	}
}

func TestDecrypt_TooShort(t *testing.T) {
	mustInit(t)

	// Encode fewer bytes than NonceSize.
	short := base64.StdEncoding.EncodeToString(make([]byte, aegis256.NonceSize-1))
	if _, err := Decrypt(short); err == nil {
		t.Error("expected error for ciphertext shorter than nonce")
	}
}

func TestDecrypt_WrongKey(t *testing.T) {
	if err := Init(validKey); err != nil {
		t.Fatalf("Init: %v", err)
	}
	ciphertext, err := Encrypt("secret")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if err := Init("completely-different-secret"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := Decrypt(ciphertext); err == nil {
		t.Error("expected error when decrypting with a different key")
	}
}

func TestDeriveKey_Deterministic(t *testing.T) {
	k1, err := deriveKey("mysecret", "info")
	if err != nil {
		t.Fatalf("deriveKey: %v", err)
	}
	k2, err := deriveKey("mysecret", "info")
	if err != nil {
		t.Fatalf("deriveKey: %v", err)
	}
	if string(k1) != string(k2) {
		t.Error("expected deriveKey to be deterministic")
	}
}

func TestDeriveKey_DifferentInfoDifferentKey(t *testing.T) {
	k1, err := deriveKey("mysecret", "info-a")
	if err != nil {
		t.Fatalf("deriveKey: %v", err)
	}
	k2, err := deriveKey("mysecret", "info-b")
	if err != nil {
		t.Fatalf("deriveKey: %v", err)
	}
	if string(k1) == string(k2) {
		t.Error("expected different keys for different info strings")
	}
}

func TestDeriveKey_DifferentSecretDifferentKey(t *testing.T) {
	k1, err := deriveKey("secret-a", "info")
	if err != nil {
		t.Fatalf("deriveKey: %v", err)
	}
	k2, err := deriveKey("secret-b", "info")
	if err != nil {
		t.Fatalf("deriveKey: %v", err)
	}
	if string(k1) == string(k2) {
		t.Error("expected different keys for different secrets")
	}
}
