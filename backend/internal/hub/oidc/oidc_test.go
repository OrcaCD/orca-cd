package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"
)

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v) //nolint:errcheck,gosec
}

func initCrypto(t *testing.T) {
	t.Helper()
	if err := crypto.Init("test-secret-that-is-long-enough-32chars"); err != nil {
		t.Fatalf("crypto.Init() error: %v", err)
	}
}

func TestBuildOAuth2Config_DefaultScopes(t *testing.T) {
	provider := &models.OIDCProvider{
		Base:         models.Base{Id: "prov-1"},
		ClientId:     "my-client",
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
		ClientId:     "my-client",
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

func TestNormalizePictureURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "valid https", in: "https://cdn.example.com/avatar.png", want: "https://cdn.example.com/avatar.png"},
		{name: "valid http", in: "http://cdn.example.com/avatar.png", want: "http://cdn.example.com/avatar.png"},
		{name: "empty", in: "", want: ""},
		{name: "relative path", in: "/avatar.png", want: ""},
		{name: "javascript scheme", in: "javascript:alert(1)", want: ""},
		{name: "malformed url", in: "http://", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizePictureURL(tt.in)
			if got != tt.want {
				t.Errorf("normalizePictureURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestEncryptDecryptState_Roundtrip(t *testing.T) {
	initCrypto(t)

	original := &stateData{
		State:      "random-state-value",
		Verifier:   "pkce-verifier-value",
		ProviderId: "prov-123",
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
	if decrypted.ProviderId != original.ProviderId {
		t.Errorf("ProviderId: expected %q, got %q", original.ProviderId, decrypted.ProviderId)
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

// HandleCallback state validation tests

func makeEncryptedState(t *testing.T, sd *stateData) string {
	t.Helper()
	enc, err := encryptState(sd)
	if err != nil {
		t.Fatalf("encryptState() error: %v", err)
	}
	return enc
}

func TestHandleCallback_ExpiredState(t *testing.T) {
	initCrypto(t)

	provider := &models.OIDCProvider{Base: models.Base{Id: "prov-1"}}
	sd := &stateData{State: "s", Verifier: "v", ProviderId: "prov-1", ExpiresAt: time.Now().Add(-1 * time.Minute).Unix()}
	enc := makeEncryptedState(t, sd)

	_, err := HandleCallback(t.Context(), provider, "http://localhost", "code", "s", enc)
	if err == nil || !strings.Contains(err.Error(), "state expired") {
		t.Errorf("expected 'state expired' error, got: %v", err)
	}
}

func TestHandleCallback_StateMismatch(t *testing.T) {
	initCrypto(t)

	provider := &models.OIDCProvider{Base: models.Base{Id: "prov-1"}}
	sd := &stateData{State: "correct-state", Verifier: "v", ProviderId: "prov-1", ExpiresAt: time.Now().Add(5 * time.Minute).Unix()}
	enc := makeEncryptedState(t, sd)

	_, err := HandleCallback(t.Context(), provider, "http://localhost", "code", "wrong-state", enc)
	if err == nil || !strings.Contains(err.Error(), "state mismatch") {
		t.Errorf("expected 'state mismatch' error, got: %v", err)
	}
}

func TestHandleCallback_ProviderMismatch(t *testing.T) {
	initCrypto(t)

	provider := &models.OIDCProvider{Base: models.Base{Id: "prov-DIFFERENT"}}
	sd := &stateData{State: "s", Verifier: "v", ProviderId: "prov-1", ExpiresAt: time.Now().Add(5 * time.Minute).Unix()}
	enc := makeEncryptedState(t, sd)

	_, err := HandleCallback(t.Context(), provider, "http://localhost", "code", "s", enc)
	if err == nil || !strings.Contains(err.Error(), "provider mismatch") {
		t.Errorf("expected 'provider mismatch' error, got: %v", err)
	}
}

func TestHandleCallback_InvalidStateCookie(t *testing.T) {
	initCrypto(t)

	provider := &models.OIDCProvider{Base: models.Base{Id: "prov-1"}}
	_, err := HandleCallback(t.Context(), provider, "http://localhost", "code", "s", "garbage-data")
	if err == nil || !strings.Contains(err.Error(), "invalid state cookie") {
		t.Errorf("expected 'invalid state cookie' error, got: %v", err)
	}
}

// Test OIDC server for full flow tests

type testOIDCServer struct {
	Server     *httptest.Server
	PrivateKey *rsa.PrivateKey
	KeyId      string
}

func newTestOIDCServer(t *testing.T) *testOIDCServer {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	kid := "test-key-1"

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// OIDC discovery
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"issuer":                                srv.URL,
			"authorization_endpoint":                srv.URL + "/authorize",
			"token_endpoint":                        srv.URL + "/token",
			"jwks_uri":                              srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})

	// JWKS
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"keys": []map[string]any{{
				"kty": "RSA",
				"alg": "RS256",
				"use": "sig",
				"kid": kid,
				"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
			}},
		})
	})

	// Token endpoint
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, _ *http.Request) {
		idToken := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
			"iss":            srv.URL,
			"sub":            "oidc-user-42",
			"aud":            "test-client-id",
			"exp":            time.Now().Add(time.Hour).Unix(),
			"iat":            time.Now().Unix(),
			"nonce":          "test-nonce-value",
			"email":          "oidc@example.com",
			"email_verified": true,
			"name":           "OIDC User",
			"picture":        "https://cdn.example.com/oidc-user-42.png",
		})
		idToken.Header["kid"] = kid
		signed, _ := idToken.SignedString(key)

		writeJSON(w, map[string]any{
			"access_token": "test-access-token",
			"token_type":   "Bearer",
			"id_token":     signed,
		})
	})

	return &testOIDCServer{Server: srv, PrivateKey: key, KeyId: kid}
}

func (s *testOIDCServer) URL() string { return s.Server.URL }

func TestStartAuth_Success(t *testing.T) {
	initCrypto(t)
	srv := newTestOIDCServer(t)

	provider := &models.OIDCProvider{
		Base:         models.Base{Id: "prov-test"},
		ClientId:     "test-client-id",
		ClientSecret: crypto.EncryptedString("test-client-secret"),
		IssuerURL:    srv.URL(),
		Scopes:       "",
	}

	authURL, encState, err := StartAuth(context.Background(), provider, "https://app.example.com")
	if err != nil {
		t.Fatalf("StartAuth() error: %v", err)
	}

	// Auth URL should point to the test server's authorize endpoint
	if !strings.HasPrefix(authURL, srv.URL()+"/authorize") {
		t.Errorf("auth URL should start with %s/authorize, got: %s", srv.URL(), authURL)
	}

	// Should contain PKCE code_challenge
	if !strings.Contains(authURL, "code_challenge=") {
		t.Error("auth URL missing PKCE code_challenge parameter")
	}
	if !strings.Contains(authURL, "code_challenge_method=S256") {
		t.Error("auth URL missing code_challenge_method=S256")
	}

	// Encrypted state should be decryptable and valid
	sd, err := decryptState(encState)
	if err != nil {
		t.Fatalf("decryptState() error: %v", err)
	}
	if sd.ProviderId != "prov-test" {
		t.Errorf("state ProviderId: expected prov-test, got %s", sd.ProviderId)
	}
	if sd.Verifier == "" {
		t.Error("state Verifier should not be empty")
	}
	if sd.ExpiresAt <= time.Now().Unix() {
		t.Error("state should not already be expired")
	}
}

func TestHandleCallback_Success(t *testing.T) {
	initCrypto(t)
	srv := newTestOIDCServer(t)

	provider := &models.OIDCProvider{
		Base:         models.Base{Id: "prov-test"},
		ClientId:     "test-client-id",
		ClientSecret: crypto.EncryptedString("test-client-secret"),
		IssuerURL:    srv.URL(),
	}

	// Build a valid encrypted state with matching params
	sd := &stateData{
		State:      "test-state-value",
		Verifier:   oauth2.GenerateVerifier(),
		Nonce:      "test-nonce-value",
		ProviderId: "prov-test",
		ExpiresAt:  time.Now().Add(5 * time.Minute).Unix(),
	}
	encState := makeEncryptedState(t, sd)

	user, err := HandleCallback(context.Background(), provider, "https://app.example.com", "test-auth-code", "test-state-value", encState)
	if err != nil {
		t.Fatalf("HandleCallback() error: %v", err)
	}

	if user.Subject != "oidc-user-42" {
		t.Errorf("Subject: expected %q, got %q", "oidc-user-42", user.Subject)
	}
	if user.Email != "oidc@example.com" {
		t.Errorf("Email: expected %q, got %q", "oidc@example.com", user.Email)
	}
	if user.Name != "OIDC User" {
		t.Errorf("Name: expected %q, got %q", "OIDC User", user.Name)
	}
	if user.Picture != "https://cdn.example.com/oidc-user-42.png" {
		t.Errorf("Picture: expected %q, got %q", "https://cdn.example.com/oidc-user-42.png", user.Picture)
	}
	if user.Issuer != srv.URL() {
		t.Errorf("Issuer: expected %q, got %q", srv.URL(), user.Issuer)
	}
}

func TestHandleCallback_MissingEmail(t *testing.T) {
	initCrypto(t)

	// Custom server that returns a token without an email claim
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	kid := "no-email-key"

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"issuer": srv.URL, "authorization_endpoint": srv.URL + "/authorize",
			"token_endpoint": srv.URL + "/token", "jwks_uri": srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"keys": []map[string]any{{
				"kty": "RSA", "alg": "RS256", "use": "sig", "kid": kid,
				"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
			}},
		})
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, _ *http.Request) {
		idToken := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
			"iss": srv.URL, "sub": "user-no-email", "aud": "test-client",
			"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
			// No email claim!
		})
		idToken.Header["kid"] = kid
		signed, _ := idToken.SignedString(key)
		writeJSON(w, map[string]any{"access_token": "x", "token_type": "Bearer", "id_token": signed})
	})

	provider := &models.OIDCProvider{
		Base: models.Base{Id: "prov-noemail"}, ClientId: "test-client",
		ClientSecret: crypto.EncryptedString("s"), IssuerURL: srv.URL,
	}
	sd := &stateData{State: "s", Verifier: oauth2.GenerateVerifier(), ProviderId: "prov-noemail", ExpiresAt: time.Now().Add(5 * time.Minute).Unix()}
	enc := makeEncryptedState(t, sd)

	_, err := HandleCallback(context.Background(), provider, "https://app.example.com", "code", "s", enc)
	if err == nil || !strings.Contains(err.Error(), "email claim is missing") {
		t.Errorf("expected 'email claim is missing' error, got: %v", err)
	}
}

func TestHandleCallback_RequireVerifiedEmailFalseClaim(t *testing.T) {
	initCrypto(t)

	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	kid := "email-false-key"

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"issuer": srv.URL, "authorization_endpoint": srv.URL + "/authorize",
			"token_endpoint": srv.URL + "/token", "jwks_uri": srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"keys": []map[string]any{{
				"kty": "RSA", "alg": "RS256", "use": "sig", "kid": kid,
				"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
			}},
		})
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, _ *http.Request) {
		idToken := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
			"iss": srv.URL, "sub": "user-email-false", "aud": "test-client",
			"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
			"email":          "unverified@example.com",
			"email_verified": false,
		})
		idToken.Header["kid"] = kid
		signed, _ := idToken.SignedString(key)
		writeJSON(w, map[string]any{"access_token": "x", "token_type": "Bearer", "id_token": signed})
	})

	provider := &models.OIDCProvider{
		Base: models.Base{Id: "prov-email-false"}, ClientId: "test-client",
		ClientSecret: crypto.EncryptedString("s"), IssuerURL: srv.URL,
		RequireVerifiedEmail: true,
	}
	sd := &stateData{State: "s", Verifier: oauth2.GenerateVerifier(), ProviderId: "prov-email-false", ExpiresAt: time.Now().Add(5 * time.Minute).Unix()}
	enc := makeEncryptedState(t, sd)

	_, err := HandleCallback(context.Background(), provider, "https://app.example.com", "code", "s", enc)
	if !errors.Is(err, ErrEmailNotVerified) {
		t.Fatalf("expected ErrEmailNotVerified, got: %v", err)
	}
}

func TestHandleCallback_RequireVerifiedEmailMissingClaim(t *testing.T) {
	initCrypto(t)

	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	kid := "email-missing-verified-key"

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"issuer": srv.URL, "authorization_endpoint": srv.URL + "/authorize",
			"token_endpoint": srv.URL + "/token", "jwks_uri": srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"keys": []map[string]any{{
				"kty": "RSA", "alg": "RS256", "use": "sig", "kid": kid,
				"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
			}},
		})
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, _ *http.Request) {
		idToken := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
			"iss": srv.URL, "sub": "user-email-missing-verified", "aud": "test-client",
			"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
			"email": "missing-verified@example.com",
		})
		idToken.Header["kid"] = kid
		signed, _ := idToken.SignedString(key)
		writeJSON(w, map[string]any{"access_token": "x", "token_type": "Bearer", "id_token": signed})
	})

	provider := &models.OIDCProvider{
		Base: models.Base{Id: "prov-email-missing-verified"}, ClientId: "test-client",
		ClientSecret: crypto.EncryptedString("s"), IssuerURL: srv.URL,
		RequireVerifiedEmail: true,
	}
	sd := &stateData{State: "s", Verifier: oauth2.GenerateVerifier(), ProviderId: "prov-email-missing-verified", ExpiresAt: time.Now().Add(5 * time.Minute).Unix()}
	enc := makeEncryptedState(t, sd)

	_, err := HandleCallback(context.Background(), provider, "https://app.example.com", "code", "s", enc)
	if !errors.Is(err, ErrEmailNotVerified) {
		t.Fatalf("expected ErrEmailNotVerified, got: %v", err)
	}
}

func TestHandleCallback_NameFallsBackToEmail(t *testing.T) {
	initCrypto(t)

	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	kid := "no-name-key"

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"issuer": srv.URL, "authorization_endpoint": srv.URL + "/authorize",
			"token_endpoint": srv.URL + "/token", "jwks_uri": srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"keys": []map[string]any{{
				"kty": "RSA", "alg": "RS256", "use": "sig", "kid": kid,
				"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
			}},
		})
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, _ *http.Request) {
		idToken := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
			"iss": srv.URL, "sub": "user-no-name", "aud": "test-client",
			"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
			"email": "noname@example.com",
			// No name claim — should fall back to email
		})
		idToken.Header["kid"] = kid
		signed, _ := idToken.SignedString(key)
		writeJSON(w, map[string]any{"access_token": "x", "token_type": "Bearer", "id_token": signed})
	})

	provider := &models.OIDCProvider{
		Base: models.Base{Id: "prov-noname"}, ClientId: "test-client",
		ClientSecret: crypto.EncryptedString("s"), IssuerURL: srv.URL,
	}
	sd := &stateData{State: "s", Verifier: oauth2.GenerateVerifier(), ProviderId: "prov-noname", ExpiresAt: time.Now().Add(5 * time.Minute).Unix()}
	enc := makeEncryptedState(t, sd)

	user, err := HandleCallback(context.Background(), provider, "https://app.example.com", "code", "s", enc)
	if err != nil {
		t.Fatalf("HandleCallback() error: %v", err)
	}
	if user.Name != "noname@example.com" {
		t.Errorf("Name should fall back to email, got: %q", user.Name)
	}
	if user.Picture != "" {
		t.Errorf("Picture should be empty when claim is missing, got: %q", user.Picture)
	}
}
