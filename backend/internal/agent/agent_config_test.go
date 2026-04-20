package agent

import (
	"encoding/base64"
	"testing"

	"github.com/rs/zerolog"
)

// testAgentToken is a minimal JWT-format string with a known subject.
// The signature is intentionally fake; DefaultConfig only calls jwt.ParseUnverified,
// which does not check the signature.
var testAgentToken = func() string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"EdDSA","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"test-agent-id"}`))
	return header + "." + payload + ".fakesig"
}()

func TestDefaultConfig_Valid(t *testing.T) {
	t.Setenv("HUB_URL", "https://hub.example.com")
	t.Setenv("AUTH_TOKEN", testAgentToken)
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_JSON", "true")

	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.LogLevel != zerolog.DebugLevel {
		t.Errorf("LogLevel = %v, want %v", cfg.LogLevel, zerolog.DebugLevel)
	}
	if !cfg.LogJSON {
		t.Error("LogJSON = false, want true")
	}
	if cfg.HubUrl != "wss://hub.example.com/api/v1/ws" {
		t.Errorf("HubUrl = %q, want %q", cfg.HubUrl, "wss://hub.example.com/api/v1/ws")
	}
	if cfg.AuthToken != testAgentToken {
		t.Errorf("AuthToken = %q, want %q", cfg.AuthToken, testAgentToken)
	}
	if cfg.AgentID != "test-agent-id" {
		t.Errorf("AgentID = %q, want %q", cfg.AgentID, "test-agent-id")
	}
}

func TestDefaultConfig_Defaults(t *testing.T) {
	t.Setenv("HUB_URL", "https://hub.example.com")
	t.Setenv("AUTH_TOKEN", testAgentToken)
	t.Setenv("LOG_LEVEL", "")
	t.Setenv("LOG_JSON", "")

	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.LogLevel != zerolog.InfoLevel {
		t.Errorf("LogLevel = %v, want %v", cfg.LogLevel, zerolog.InfoLevel)
	}
	if cfg.LogJSON {
		t.Error("LogJSON = true, want false by default")
	}
}

func TestDefaultConfig_LogLevels(t *testing.T) {
	tests := []struct {
		input string
		want  zerolog.Level
	}{
		{"trace", zerolog.TraceLevel},
		{"debug", zerolog.DebugLevel},
		{"info", zerolog.InfoLevel},
		{"warn", zerolog.WarnLevel},
		{"error", zerolog.ErrorLevel},
		{"invalid", zerolog.InfoLevel},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Setenv("HUB_URL", "https://hub.example.com")
			t.Setenv("AUTH_TOKEN", testAgentToken)
			t.Setenv("LOG_LEVEL", tt.input)

			cfg, err := DefaultConfig()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.LogLevel != tt.want {
				t.Errorf("LogLevel = %v, want %v", cfg.LogLevel, tt.want)
			}
		})
	}
}

func TestDefaultConfig_LogJSON(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"true", true},
		{"TRUE", true},
		{"false", false},
		{"invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Setenv("HUB_URL", "https://hub.example.com")
			t.Setenv("AUTH_TOKEN", testAgentToken)
			t.Setenv("LOG_JSON", tt.input)

			cfg, err := DefaultConfig()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.LogJSON != tt.want {
				t.Errorf("LogJSON = %v, want %v", cfg.LogJSON, tt.want)
			}
		})
	}
}

func TestDefaultConfig_Errors(t *testing.T) {
	tests := []struct {
		name      string
		hubURL    string
		authToken string
	}{
		{"missing auth token", "https://hub.example.com", ""},
		{"invalid hub url", "not-a-url", testAgentToken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HUB_URL", tt.hubURL)
			t.Setenv("AUTH_TOKEN", tt.authToken)

			_, err := DefaultConfig()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}
