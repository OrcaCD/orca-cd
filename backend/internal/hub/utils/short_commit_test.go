package utils

import "testing"

func TestShortCommit(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"abc", "abc"},
		{"abcdef1", "abcdef1"},
		{"abcdef1234567890", "abcdef1"},
	}
	for _, tt := range tests {
		if got := ShortCommit(tt.in); got != tt.want {
			t.Errorf("ShortCommit(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
