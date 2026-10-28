package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/shared/agenttoken"
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
		Subject:      agentID,
		IssuedAt:     jwt.NewNumericDate(now),
		NotBefore:    jwt.NewNumericDate(now),
		Audience:     jwt.ClaimStrings{"agent"},
		HubPublicKey: base64.StdEncoding.EncodeToString(pub),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	str, err := token.SignedString(priv)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return str, pub
}

// makeTestAuthToken builds a full compound AUTH_TOKEN (JWT + appended signing
// seed) as issued by the hub, for tests exercising DefaultConfig end-to-end.
func makeTestAuthToken(t *testing.T) (authToken string, hubPubKey ed25519.PublicKey, signingPriv ed25519.PrivateKey) {
	t.Helper()
	jwtStr, hubPub := makeTestToken(t, "test-agent-id")
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 signing key: %v", err)
	}
	token, err := agenttoken.Encode(jwtStr, priv.Seed())
	if err != nil {
		t.Fatalf("agenttoken.Encode: %v", err)
	}
	return token, hubPub, priv
}

func TestDefaultConfig_Valid(t *testing.T) {
	token, _, signingPriv := makeTestAuthToken(t)
	jwtToken, _, _ := agenttoken.Decode(token)
	t.Setenv("HUB_URL", "https://hub.example.com")
	t.Setenv("AUTH_TOKEN", token)
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_JSON", "true")
	t.Setenv("DEPLOYMENTS_DIR", "/test/deployments")

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
	if cfg.AuthToken != jwtToken {
		t.Errorf("AuthToken = %q, want the bare JWT %q", cfg.AuthToken, jwtToken)
	}
	if cfg.AgentID != "test-agent-id" {
		t.Errorf("AgentID = %q, want %q", cfg.AgentID, "test-agent-id")
	}
	if len(cfg.HubPublicKey) != ed25519.PublicKeySize {
		t.Errorf("HubPublicKey length = %d, want %d", len(cfg.HubPublicKey), ed25519.PublicKeySize)
	}
	if !cfg.SigningPrivateKey.Equal(signingPriv) {
		t.Error("SigningPrivateKey does not match the key encoded into the token")
	}
	if cfg.DeploymentsDir != "/test/deployments" {
		t.Errorf("DeploymentsDir = %q, want %q", cfg.DeploymentsDir, "/test/deployments")
	}
}

func TestDefaultConfig_Defaults(t *testing.T) {
	token, _, _ := makeTestAuthToken(t)
	t.Setenv("HUB_URL", "https://hub.example.com")
	t.Setenv("AUTH_TOKEN", token)
	t.Setenv("LOG_LEVEL", "")
	t.Setenv("LOG_JSON", "")
	t.Setenv("DEPLOYMENTS_DIR", "")
	t.Setenv("CA_CERT_FILE", "")

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
	if cfg.DeploymentsDir != "/deployments" {
		t.Errorf("DeploymentsDir = %q, want %q", cfg.DeploymentsDir, "/deployments")
	}
	if cfg.RootCAs != nil {
		t.Error("RootCAs should be nil when CA_CERT_FILE is not set")
	}
}

func TestDefaultConfig_DeploymentsDirIsAbsolute(t *testing.T) {
	token, _, _ := makeTestAuthToken(t)
	t.Setenv("HUB_URL", "https://hub.example.com")
	t.Setenv("AUTH_TOKEN", token)
	t.Setenv("DEPLOYMENTS_DIR", "deployments/../apps/")
	t.Chdir(t.TempDir())

	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if want := filepath.Join(wd, "apps"); cfg.DeploymentsDir != want {
		t.Errorf("DeploymentsDir = %q, want %q", cfg.DeploymentsDir, want)
	}
}

// writeCertPEM writes cert as a PEM file into a temp dir and returns its path.
func writeCertPEM(t *testing.T, cert *x509.Certificate) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ca.pem")
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write cert file: %v", err)
	}
	return path
}

func TestDefaultConfig_CACertFile(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)

	token, _, _ := makeTestAuthToken(t)
	t.Setenv("HUB_URL", "https://hub.example.com")
	t.Setenv("AUTH_TOKEN", token)
	t.Setenv("CA_CERT_FILE", writeCertPEM(t, srv.Certificate()))

	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RootCAs == nil {
		t.Fatal("RootCAs should be set when CA_CERT_FILE is configured")
	}
	if _, err := srv.Certificate().Verify(x509.VerifyOptions{Roots: cfg.RootCAs}); err != nil {
		t.Errorf("configured CA should be trusted: %v", err)
	}
}

func TestDefaultConfig_CACertFileErrors(t *testing.T) {
	invalidPEM := filepath.Join(t.TempDir(), "invalid.pem")
	if err := os.WriteFile(invalidPEM, []byte("not a certificate"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tests := []struct {
		name string
		path string
	}{
		{"missing file", filepath.Join(t.TempDir(), "missing.pem")},
		{"no PEM certificates", invalidPEM},
	}

	token, _, _ := makeTestAuthToken(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HUB_URL", "https://hub.example.com")
			t.Setenv("AUTH_TOKEN", token)
			t.Setenv("CA_CERT_FILE", tt.path)

			_, err := DefaultConfig()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if _, ok := errors.AsType[*FatalConfigError](err); !ok {
				t.Errorf("expected *FatalConfigError, got %T: %v", err, err)
			}
		})
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
			token, _, _ := makeTestAuthToken(t)
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
			token, _, _ := makeTestAuthToken(t)
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

func TestDefaultConfig_RestrictBindMountsToDeployDir(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"true", true},
		{"TRUE", true},
		{"1", true},
		{"yes", true},
		{"false", false},
		{"0", false},
		{"invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			token, _, _ := makeTestAuthToken(t)
			t.Setenv("HUB_URL", "https://hub.example.com")
			t.Setenv("AUTH_TOKEN", token)
			t.Setenv("RESTRICT_VOLUMES_TO_DEPLOYMENTS_DIR", tt.input)

			cfg, err := DefaultConfig()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.RestrictBindMountsToDeployDir != tt.want {
				t.Errorf("RestrictBindMountsToDeployDir = %v, want %v", cfg.RestrictBindMountsToDeployDir, tt.want)
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

func TestSenderRef_LoadNilWhenEmpty(t *testing.T) {
	var ref senderRef
	if got := ref.load(); got != nil {
		t.Errorf("expected nil from empty senderRef, got %v", got)
	}
}

func TestSenderRef_SendMessageNilSender(t *testing.T) {
	var ref senderRef
	if err := ref.SendMessage(nil); err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestSenderRef_StoreAndLoad(t *testing.T) {
	var ref senderRef
	sender := &stubSender{}
	ref.store(sender)
	if got := ref.load(); got == nil {
		t.Error("expected non-nil sender after store")
	}
}

func TestSenderRef_SendMessageDelegates(t *testing.T) {
	var ref senderRef
	sent := make(chan *messages.ClientMessage, 1)
	sender := &stubSender{sent: sent}
	ref.store(sender)

	msg := &messages.ClientMessage{
		Payload: &messages.ClientMessage_Pong{Pong: &messages.PongResponse{Timestamp: 99}},
	}
	if err := ref.SendMessage(msg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case got := <-sent:
		if got != msg {
			t.Error("expected delegated message to match input")
		}
	default:
		t.Error("expected sender to receive the message")
	}
}

func TestDefaultConfig_MissingSigningKeySegment(t *testing.T) {
	// A bare 3-segment JWT (pre-compound-token format) must be rejected.
	bareJWT, _ := makeTestToken(t, "test-agent-id")
	t.Setenv("HUB_URL", "https://hub.example.com")
	t.Setenv("AUTH_TOKEN", bareJWT)

	_, err := DefaultConfig()
	if err == nil {
		t.Fatal("expected error for token missing signing-key segment")
	}
	if _, ok := errors.AsType[*FatalConfigError](err); !ok {
		t.Errorf("expected *FatalConfigError, got %T: %v", err, err)
	}
}

func TestDefaultConfig_Errors(t *testing.T) {
	validToken, _, _ := makeTestAuthToken(t)
	tests := []struct {
		name      string
		hubURL    string
		authToken string
		fatal     bool
	}{
		{"missing hub url", "", validToken, true},
		{"missing auth token", "https://hub.example.com", "", true},
		{"invalid hub url", "not-a-url", validToken, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HUB_URL", tt.hubURL)
			t.Setenv("AUTH_TOKEN", tt.authToken)

			_, err := DefaultConfig()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			var fatalErr *FatalConfigError
			if got := errors.As(err, &fatalErr); got != tt.fatal {
				t.Errorf("FatalConfigError = %v, want %v", got, tt.fatal)
			}
		})
	}
}

func TestClearSecretEnv(t *testing.T) {
	t.Setenv("AUTH_TOKEN", "secret")
	t.Setenv("APP_SECRET", "hub-secret")
	t.Setenv("OLD_APP_SECRET", "old-hub-secret")
	t.Setenv("HUB_URL", "https://hub.example.com")

	foundHubSecrets, err := clearSecretEnv()
	if err != nil {
		t.Fatalf("clearSecretEnv: %v", err)
	}
	for _, name := range []string{"AUTH_TOKEN", "APP_SECRET", "OLD_APP_SECRET"} {
		if _, ok := os.LookupEnv(name); ok {
			t.Fatalf("expected %s to be removed from the environment", name)
		}
	}
	if !slices.Equal(foundHubSecrets, []string{"APP_SECRET", "OLD_APP_SECRET"}) {
		t.Fatalf("expected the hub secrets to be reported, got %v", foundHubSecrets)
	}
	if os.Getenv("HUB_URL") == "" {
		t.Fatal("expected non-secret settings to stay in the environment")
	}
}

func TestClearSecretEnv_ReportsNoHubSecretsWhenAbsent(t *testing.T) {
	t.Setenv("AUTH_TOKEN", "secret")
	// t.Setenv registers the cleanup that restores the variables after unsetting them.
	t.Setenv("APP_SECRET", "")
	t.Setenv("OLD_APP_SECRET", "")
	if err := os.Unsetenv("APP_SECRET"); err != nil {
		t.Fatal(err)
	}
	if err := os.Unsetenv("OLD_APP_SECRET"); err != nil {
		t.Fatal(err)
	}

	foundHubSecrets, err := clearSecretEnv()
	if err != nil {
		t.Fatalf("clearSecretEnv: %v", err)
	}
	if len(foundHubSecrets) != 0 {
		t.Fatalf("expected no hub secrets to be reported, got %v", foundHubSecrets)
	}
}
