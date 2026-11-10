package utils

import "testing"

func TestCapitalizeFirst(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"the changes were saved", "The changes were saved"},
		{"Already", "Already"},
		{"émile", "Émile"},
		{"1abc", "1abc"},
		{"\xffabc", "\xffabc"},
	}
	for _, tt := range tests {
		if got := CapitalizeFirst(tt.in); got != tt.want {
			t.Errorf("CapitalizeFirst(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
