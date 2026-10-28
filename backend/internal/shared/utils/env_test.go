package utils

import "testing"

func TestParseBool(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"1", true},
		{"t", true},
		{"T", true},
		{"true", true},
		{"TRUE", true},
		{"True", true},
		{"y", true},
		{"yes", true},
		{"YES", true},
		{"on", true},
		{"On", true},
		{"  yes  ", true},
		{"0", false},
		{"f", false},
		{"false", false},
		{"no", false},
		{"off", false},
		{"invalid", false},
		{"", false},
		{"   ", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := ParseBool(tt.input); got != tt.want {
				t.Errorf("ParseBool(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestEnvBool(t *testing.T) {
	t.Setenv("ORCA_TEST_BOOL", "yes")
	if !EnvBool("ORCA_TEST_BOOL") {
		t.Error("EnvBool = false, want true")
	}

	t.Setenv("ORCA_TEST_BOOL", "")
	if EnvBool("ORCA_TEST_BOOL") {
		t.Error("EnvBool = true, want false for empty value")
	}
}
