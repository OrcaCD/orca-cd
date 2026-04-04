package agent

import "testing"

func TestParseHubURL_Valid(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// http/https are converted to ws/wss
		{"http://hub.example.com", "ws://hub.example.com/api/v1/ws"},
		{"https://hub.example.com", "wss://hub.example.com/api/v1/ws"},
		// ws/wss pass through unchanged
		{"ws://hub.example.com", "ws://hub.example.com/api/v1/ws"},
		{"wss://hub.example.com", "wss://hub.example.com/api/v1/ws"},
		// trailing slash is treated as no path → default path appended
		{"https://hub.example.com/", "wss://hub.example.com/api/v1/ws"},
		// explicit path is preserved
		{"https://hub.example.com/api/v1/ws", "wss://hub.example.com/api/v1/ws"},
		{"https://hub.example.com/custom/ws", "wss://hub.example.com/custom/ws"},
		// non-standard ports are preserved
		{"http://hub.example.com:8080", "ws://hub.example.com:8080/api/v1/ws"},
		{"https://hub.example.com:8443", "wss://hub.example.com:8443/api/v1/ws"},
		// scheme case is normalised
		{"HTTP://hub.example.com", "ws://hub.example.com/api/v1/ws"},
		{"HTTPS://hub.example.com", "wss://hub.example.com/api/v1/ws"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseHubURL(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("parseHubURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseHubURL_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"no scheme", "hub.example.com"},
		{"no host", "https://"},
		{"unsupported scheme", "ftp://hub.example.com"},
		{"with query", "https://hub.example.com?foo=bar"},
		{"with fragment", "https://hub.example.com#section"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseHubURL(tt.input)
			if err == nil {
				t.Errorf("parseHubURL(%q) expected error, got nil", tt.input)
			}
		})
	}
}
