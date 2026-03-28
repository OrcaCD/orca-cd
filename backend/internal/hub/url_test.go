package hub

import "testing"

func TestParseAppURL_Valid(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://example.com", "https://example.com"},
		{"http://example.com", "http://example.com"},
		{"https://Example.COM", "https://example.com"},
		{"https://example.com/", "https://example.com"},
		{"https://example.com:8443", "https://example.com:8443"},
		{"http://example.com:3000", "http://example.com:3000"},
		{"https://example.com:443", "https://example.com"},
		{"http://example.com:80", "http://example.com"},
		{"HTTP://EXAMPLE.COM", "http://example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseAppURL(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("parseAppURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseAppURL_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"no scheme", "example.com"},
		{"no host", "https://"},
		{"with path", "https://example.com/dashboard"},
		{"with query", "https://example.com?foo=bar"},
		{"with fragment", "https://example.com#section"},
		{"path and query", "https://example.com/path?q=1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseAppURL(tt.input)
			if err == nil {
				t.Errorf("parseAppURL(%q) expected error, got nil", tt.input)
			}
		})
	}
}
