package models

import "testing"

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"lowercases", "MyApp", "myapp"},
		{"trims whitespace", "  spaced  ", "spaced"},
		{"trims and lowercases", "  Mixed Case  ", "mixed case"},
		{"empty stays empty", "", ""},
		{"only whitespace", "   ", ""},
		{"already normalized", "app", "app"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeName(tt.in); got != tt.want {
				t.Errorf("NormalizeName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestApplicationTableName(t *testing.T) {
	if got := (Application{}).TableName(); got != "applications" {
		t.Errorf("TableName() = %q, want %q", got, "applications")
	}
}
