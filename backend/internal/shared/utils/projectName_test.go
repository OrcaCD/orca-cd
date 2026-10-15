package utils

import "testing"

func TestNormalizeProjectName(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"billing", "billing", false},
		{"My App", "my-app", false},
		{"orcacd docs", "orcacd-docs", false},
		{"test/orcacd-docs", "test-orcacd-docs", false},
		{"Hello World!", "hello-world", false},
		{"  ---  ", "", true},
		{"___", "", true},
		{"123app", "123app", false},
	}
	for _, tt := range tests {
		got, err := NormalizeProjectName(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Errorf("NormalizeProjectName(%q): expected error, got %q", tt.input, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizeProjectName(%q): unexpected error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("NormalizeProjectName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
