package envrules

import (
	"strings"
	"testing"
)

func TestValidateKey(t *testing.T) {
	valid := []string{"A", "LOG_LEVEL", "spring.profile", "my-key", "list[0]", "1ABC", ".A", "-A", "ключ", "klíč"}
	for _, key := range valid {
		if err := ValidateKey(key); err != nil {
			t.Errorf("expected %q to be valid, got %v", key, err)
		}
	}

	invalid := []string{"", "A B", "A=B", "A:B", "A\nB", " A", "A ", "A$", "A#", "A\"", "A\xff", "A\x00"}
	for _, key := range invalid {
		if err := ValidateKey(key); err == nil {
			t.Errorf("expected %q to be invalid", key)
		}
	}
}

func TestValidateValue(t *testing.T) {
	valid := []string{"", "plain", "with space", "tab\there", "multi\nline\r\nvalue", "ünïcödé ✓", strings.Repeat("x", MaxValueBytes)}
	for _, value := range valid {
		if err := ValidateValue(value); err != nil {
			t.Errorf("expected %.20q to be valid, got %v", value, err)
		}
	}

	invalid := []string{strings.Repeat("x", MaxValueBytes+1), "bad\xff", "nul\x00", "bell\a", "escape\x1b", "del\x7f"}
	for _, value := range invalid {
		if err := ValidateValue(value); err == nil {
			t.Errorf("expected %.20q to be invalid", value)
		}
	}
}

func TestValidateValue_ErrorDoesNotContainValue(t *testing.T) {
	const secret = "do-not-leak-me"
	for _, value := range []string{secret + "\xff", secret + "\x00", secret + strings.Repeat("x", MaxValueBytes)} {
		err := ValidateValue(value)
		if err == nil {
			t.Fatalf("expected an error for %.30q", value)
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error contains the value: %v", err)
		}
	}
}
