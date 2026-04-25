package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

func makeTestToken(t *testing.T, agentID string) (tokenStr string, hubPubKey ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key: %v", err)
	}
	now := time.Now()
	claims := agentTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   agentID,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Audience:  jwt.ClaimStrings{"agent"},
		},
		HubPublicKey: base64.StdEncoding.EncodeToString(pub),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	str, err := token.SignedString(priv)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return str, pub
}

func TestDefaultConfig_Valid(t *testing.T) {
	token, _ := makeTestToken(t, "test-agent-id")
	t.Setenv("HUB_URL", "https://hub.example.com")
	t.Setenv("AUTH_TOKEN", token)
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
	if cfg.AuthToken != token {
		t.Errorf("AuthToken not set correctly")
	}
	if cfg.AgentID != "test-agent-id" {
		t.Errorf("AgentID = %q, want %q", cfg.AgentID, "test-agent-id")
	}
	if len(cfg.HubPublicKey) != ed25519.PublicKeySize {
		t.Errorf("HubPublicKey length = %d, want %d", len(cfg.HubPublicKey), ed25519.PublicKeySize)
	}
}

func TestDefaultConfig_Defaults(t *testing.T) {
	token, _ := makeTestToken(t, "test-agent-id")
	t.Setenv("HUB_URL", "https://hub.example.com")
	t.Setenv("AUTH_TOKEN", token)
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
			token, _ := makeTestToken(t, "test-agent-id")
			t.Setenv("HUB_URL", "https://hub.example.com")
			t.Setenv("AUTH_TOKEN", token)
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
			token, _ := makeTestToken(t, "test-agent-id")
			t.Setenv("HUB_URL", "https://hub.example.com")
			t.Setenv("AUTH_TOKEN", token)
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

func TestParseTokenClaims_MissingHubPublicKey(t *testing.T) {
	// JWT without hub_pubkey claim — must be rejected.
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	_ = pub
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   "agent-id",
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		Audience:  jwt.ClaimStrings{"agent"},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	tokenStr, err := token.SignedString(priv)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	_, _, err = parseTokenClaims(tokenStr)
	if err == nil {
		t.Fatal("expected error for token without hub_pubkey claim")
	}
}

func TestParseTokenClaims_MissingSubject(t *testing.T) {
	// JWT with hub_pubkey but no subject — must be rejected.
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Now()
	claims := agentTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Audience:  jwt.ClaimStrings{"agent"},
			// Subject intentionally omitted
		},
		HubPublicKey: base64.StdEncoding.EncodeToString(pub),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	tokenStr, err := token.SignedString(priv)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	_, _, err = parseTokenClaims(tokenStr)
	if err == nil {
		t.Fatal("expected error for token with missing subject")
	}
}

func TestParseTokenClaims_InvalidToken(t *testing.T) {
	_, _, err := parseTokenClaims("not.a.jwt")
	if err == nil {
		t.Fatal("expected error for malformed JWT")
	}
}

func TestParseTokenClaims_WrongSignature(t *testing.T) {
	// Token signed with key-A but hub_pubkey contains key-B — signature check must fail.
	pubA, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key A: %v", err)
	}
	_, privB, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key B: %v", err)
	}
	now := time.Now()
	claims := agentTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "agent-id",
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Audience:  jwt.ClaimStrings{"agent"},
		},
		// hub_pubkey is key-A but the token is signed with key-B
		HubPublicKey: base64.StdEncoding.EncodeToString(pubA),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	tokenStr, err := token.SignedString(privB)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	_, _, err = parseTokenClaims(tokenStr)
	if err == nil {
		t.Fatal("expected error when JWT is signed by a different key than hub_pubkey")
	}
}

func TestParseTokenClaims_Success(t *testing.T) {
	tokenStr, expectedPubKey := makeTestToken(t, "agent-xyz")

	agentID, hubPubKey, err := parseTokenClaims(tokenStr)
	if err != nil {
		t.Fatalf("parseTokenClaims: %v", err)
	}
	if agentID != "agent-xyz" {
		t.Errorf("agentID = %q, want %q", agentID, "agent-xyz")
	}
	if len(hubPubKey) != ed25519.PublicKeySize {
		t.Errorf("hubPubKey length = %d, want %d", len(hubPubKey), ed25519.PublicKeySize)
	}
	if !hubPubKey.Equal(expectedPubKey) {
		t.Error("hubPubKey does not match expected public key")
	}
}

func TestConnTracker_SetAndCancelled_CancelledCtx(t *testing.T) {
	srv := newTestServer(t, func(serverConn *websocket.Conn) {
		serverConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)) //nolint:errcheck,gosec
		_, _, _ = serverConn.ReadMessage()
	})

	clientConn := dialServer(t, srv)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel so ctx.Err() != nil

	var tracker connTracker
	if cancelled := tracker.setAndCancelled(ctx, clientConn); !cancelled {
		t.Error("expected setAndCancelled to return true for a pre-cancelled context")
	}
}

func TestDefaultConfig_Errors(t *testing.T) {
	validToken, _ := makeTestToken(t, "test-agent-id")
	tests := []struct {
		name      string
		hubURL    string
		authToken string
	}{
		{"missing auth token", "https://hub.example.com", ""},
		{"invalid hub url", "not-a-url", validToken},
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
