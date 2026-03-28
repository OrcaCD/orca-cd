package crypto

import (
	"testing"
)

func TestEncryptedString_String(t *testing.T) {
	es := EncryptedString("hello")
	if es.String() != "hello" {
		t.Errorf("expected %q, got %q", "hello", es.String())
	}
}

func TestEncryptedString_Value_Empty(t *testing.T) {
	es := EncryptedString("")
	v, err := es.Value()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != "" {
		t.Errorf("expected empty string, got %v", v)
	}
}

func TestEncryptedString_Value_NonEmpty(t *testing.T) {
	mustInit(t)

	es := EncryptedString("secret data")
	v, err := es.Value()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("expected string, got %T", v)
	}
	if s == "secret data" {
		t.Error("Value() should return encrypted data, not plaintext")
	}
	if s == "" {
		t.Error("Value() should return non-empty encrypted data")
	}
}

func TestEncryptedString_Scan_Nil(t *testing.T) {
	var es EncryptedString
	if err := es.Scan(nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if es != "" {
		t.Errorf("expected empty string, got %q", es)
	}
}

func TestEncryptedString_Scan_EmptyString(t *testing.T) {
	var es EncryptedString
	if err := es.Scan(""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if es != "" {
		t.Errorf("expected empty string, got %q", es)
	}
}

func TestEncryptedString_Scan_EmptyBytes(t *testing.T) {
	var es EncryptedString
	if err := es.Scan([]byte("")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if es != "" {
		t.Errorf("expected empty string, got %q", es)
	}
}

func TestEncryptedString_Scan_UnsupportedType(t *testing.T) {
	var es EncryptedString
	if err := es.Scan(12345); err == nil {
		t.Error("expected error for unsupported type")
	}
}

func TestEncryptedString_Scan_InvalidCiphertext(t *testing.T) {
	mustInit(t)

	var es EncryptedString
	if err := es.Scan("not-valid-base64-ciphertext!!!"); err == nil {
		t.Error("expected error for invalid ciphertext")
	}
}

func TestEncryptedString_RoundTrip(t *testing.T) {
	mustInit(t)

	original := EncryptedString("round trip test")
	v, err := original.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}

	var restored EncryptedString
	if err := restored.Scan(v); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if restored != original {
		t.Errorf("round-trip mismatch: got %q, want %q", restored, original)
	}
}

func TestEncryptedString_RoundTrip_Bytes(t *testing.T) {
	mustInit(t)

	original := EncryptedString("bytes scan test")
	v, err := original.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	// Simulate scanning as []byte (some drivers return bytes)
	var restored EncryptedString
	if err := restored.Scan([]byte(v.(string))); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if restored != original {
		t.Errorf("round-trip mismatch: got %q, want %q", restored, original)
	}
}
