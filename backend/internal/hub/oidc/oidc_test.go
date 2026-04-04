package oidc

import (
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"golang.org/x/oauth2"
)

func initCrypto(t *testing.T) {
	t.Helper()
	if err := crypto.Init("test-secret-that-is-long-enough-32chars"); err != nil {
		t.Fatalf("crypto.Init() error: %v", err)
	}
}

func TestBuildOAuth2Config_DefaultScopes(t *testing.T) {
	provider := &models.OIDCProvider{
		Base:         models.Base{Id: "prov-1"},
		ClientID:     "my-client",
		ClientSecret: crypto.EncryptedString("my-secret"),
		Scopes:       "",
	}

	endpoint := oauth2.Endpoint{AuthURL: "https://idp.example.com/auth", TokenURL: "https://idp.example.com/token"} //nolint:gosec // test data
	cfg := buildOAuth2Config(provider, endpoint, "https://app.example.com")

	if cfg.ClientID != "my-client" {
		t.Errorf("expected ClientID=my-client, got %s", cfg.ClientID)
	}
	if cfg.RedirectURL != "https://app.example.com/api/v1/auth/oidc/prov-1/callback" {
		t.Errorf("unexpected redirect URL: %s", cfg.RedirectURL)
	}

	// Default scopes: openid, email, profile
	if len(cfg.Scopes) != 3 {
		t.Fatalf("expected 3 default scopes, got %d: %v", len(cfg.Scopes), cfg.Scopes)
	}
	expected := []string{"openid", "email", "profile"}
	for i, s := range expected {
		if cfg.Scopes[i] != s {
			t.Errorf("scope[%d]: expected %q, got %q", i, s, cfg.Scopes[i])
		}
	}
}

func TestBuildOAuth2Config_ExtraScopes(t *testing.T) {
	provider := &models.OIDCProvider{
		Base:         models.Base{Id: "prov-2"},
		ClientID:     "my-client",
		ClientSecret: crypto.EncryptedString("my-secret"),
		Scopes:       "groups, offline_access",
	}

	endpoint := oauth2.Endpoint{AuthURL: "https://idp.example.com/auth", TokenURL: "https://idp.example.com/token"} //nolint:gosec // test data
	cfg := buildOAuth2Config(provider, endpoint, "https://app.example.com")

	expectedScopes := []string{"openid", "email", "profile", "groups", "offline_access"}
	if len(cfg.Scopes) != len(expectedScopes) {
		t.Fatalf("expected %d scopes, got %d: %v", len(expectedScopes), len(cfg.Scopes), cfg.Scopes)
	}
	for i, s := range expectedScopes {
		if cfg.Scopes[i] != s {
			t.Errorf("scope[%d]: expected %q, got %q", i, s, cfg.Scopes[i])
		}
	}
}

func TestEncryptDecryptState_Roundtrip(t *testing.T) {
	initCrypto(t)

	original := &stateData{
		State:      "random-state-value",
		Verifier:   "pkce-verifier-value",
		ProviderID: "prov-123",
		ExpiresAt:  time.Now().Add(10 * time.Minute).Unix(),
	}

	encrypted, err := encryptState(original)
	if err != nil {
		t.Fatalf("encryptState() error: %v", err)
	}
	if encrypted == "" {
		t.Fatal("encrypted string should not be empty")
	}

	decrypted, err := decryptState(encrypted)
	if err != nil {
		t.Fatalf("decryptState() error: %v", err)
	}

	if decrypted.State != original.State {
		t.Errorf("State: expected %q, got %q", original.State, decrypted.State)
	}
	if decrypted.Verifier != original.Verifier {
		t.Errorf("Verifier: expected %q, got %q", original.Verifier, decrypted.Verifier)
	}
	if decrypted.ProviderID != original.ProviderID {
		t.Errorf("ProviderID: expected %q, got %q", original.ProviderID, decrypted.ProviderID)
	}
	if decrypted.ExpiresAt != original.ExpiresAt {
		t.Errorf("ExpiresAt: expected %d, got %d", original.ExpiresAt, decrypted.ExpiresAt)
	}
}

func TestDecryptState_InvalidData(t *testing.T) {
	initCrypto(t)

	_, err := decryptState("not-valid-base64-ciphertext!!!")
	if err == nil {
		t.Error("expected error for invalid encrypted data")
	}
}

func TestGenerateRandomString(t *testing.T) {
	s1, err := generateRandomString()
	if err != nil {
		t.Fatalf("generateRandomString() error: %v", err)
	}
	s2, err := generateRandomString()
	if err != nil {
		t.Fatalf("generateRandomString() error: %v", err)
	}

	if s1 == "" || s2 == "" {
		t.Error("generated strings should not be empty")
	}
	if s1 == s2 {
		t.Error("two generated strings should differ")
	}
}

func TestStateCookieName(t *testing.T) {
	if got := StateCookieName(); got != "orcacd_oidc_state" {
		t.Errorf("expected %q, got %q", "orcacd_oidc_state", got)
	}
}
