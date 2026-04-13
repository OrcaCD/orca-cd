package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/golang-jwt/jwt/v5"
)

func TestInit(t *testing.T) {
	if err := initJWT("test-secret-that-is-long-enough-32chars", "http://localhost:8080"); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
}

func TestGenerateAndValidateUserToken(t *testing.T) {
	if err := initJWT("test-secret-that-is-long-enough-32chars", "http://localhost:8080"); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	user := &models.User{Base: models.Base{Id: "user-123"}, Name: "test", Email: "test@example.com"}
	token, err := GenerateUserToken(user)
	if err != nil {
		t.Fatalf("GenerateUserToken() error: %v", err)
	}
	if token == "" {
		t.Fatal("GenerateUserToken() returned empty token")
	}

	claims, err := ValidateUserToken(token)
	if err != nil {
		t.Fatalf("ValidateUserToken() error: %v", err)
	}
	if claims.Issuer != "http://localhost:8080" {
		t.Errorf("expected Issuer %q, got %q", "http://localhost:8080", claims.Issuer)
	}
	if claims.Subject != "user-123" {
		t.Errorf("expected Subject %q, got %q", "user-123", claims.Subject)
	}
	if claims.Name != "test" {
		t.Errorf("expected Name %q, got %q", "test", claims.Name)
	}
	if claims.Email != "test@example.com" {
		t.Errorf("expected Email %q, got %q", "test@example.com", claims.Email)
	}
	if claims.Picture != "" {
		t.Errorf("expected Picture to be empty, got %q", claims.Picture)
	}
	if claims.NotBefore == nil {
		t.Error("expected NotBefore to be set")
	}
	if len(claims.Audience) != 1 || claims.Audience[0] != "user" {
		t.Errorf("expected Audience [\"user\"], got %v", claims.Audience)
	}
	if claims.PasswordChangeRequired {
		t.Error("expected PasswordChangeRequired=false by default")
	}
	if claims.IsLocal {
		t.Error("expected IsLocal=false without password hash")
	}
}

func TestGenerateAndValidateUserToken_PasswordChangeRequired(t *testing.T) {
	if err := initJWT("test-secret-that-is-long-enough-32chars", "http://localhost:8080"); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	hash := "hashed-password"

	user := &models.User{
		Base:                   models.Base{Id: "user-123"},
		Name:                   "test",
		Email:                  "test@example.com",
		PasswordHash:           &hash,
		PasswordChangeRequired: true,
	}
	token, err := GenerateUserToken(user)
	if err != nil {
		t.Fatalf("GenerateUserToken() error: %v", err)
	}

	claims, err := ValidateUserToken(token)
	if err != nil {
		t.Fatalf("ValidateUserToken() error: %v", err)
	}
	if !claims.PasswordChangeRequired {
		t.Error("expected PasswordChangeRequired=true")
	}
	if !claims.IsLocal {
		t.Error("expected IsLocal=true for local user")
	}
}

func TestGenerateAndValidateUserTokenWithPicture(t *testing.T) {
	if err := initJWT("test-secret-that-is-long-enough-32chars", "http://localhost:8080"); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	user := &models.User{Base: models.Base{Id: "user-123"}, Name: "test", Email: "test@example.com"}
	picture := "https://cdn.example.com/test.png"

	token, err := GenerateUserTokenWithPicture(user, picture)
	if err != nil {
		t.Fatalf("GenerateUserTokenWithPicture() error: %v", err)
	}

	claims, err := ValidateUserToken(token)
	if err != nil {
		t.Fatalf("ValidateUserToken() error: %v", err)
	}
	if claims.Picture != picture {
		t.Errorf("expected Picture %q, got %q", picture, claims.Picture)
	}
}

func TestValidateUserToken_Invalid(t *testing.T) {
	if err := initJWT("test-secret-that-is-long-enough-32chars", "http://localhost:8080"); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	_, err := ValidateUserToken("invalid.token.here")
	if err == nil {
		t.Error("ValidateUserToken() expected error for invalid token")
	}
}

func TestValidateUserToken_Expired(t *testing.T) {
	if err := initJWT("test-secret-that-is-long-enough-32chars", "http://localhost:8080"); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	now := time.Now().Add(-2 * time.Hour)
	claims := UserClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(-1 * time.Hour)),
		},
		Name: "test",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	tokenStr, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatalf("failed to create expired token: %v", err)
	}

	_, err = ValidateUserToken(tokenStr)
	if err == nil {
		t.Error("ValidateUserToken() expected error for expired token")
	}
}

func TestValidateUserToken_WrongIssuer(t *testing.T) {
	if err := initJWT("test-secret-that-is-long-enough-32chars", "http://localhost:8080"); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	now := time.Now()
	claims := UserClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "http://evil.example.com",
			Subject:   "user-123",
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
		Name: "test",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	tokenStr, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	_, err = ValidateUserToken(tokenStr)
	if err == nil {
		t.Error("ValidateUserToken() expected error for wrong issuer")
	}
}

func TestValidateUserToken_MissingSubject(t *testing.T) {
	if err := initJWT("test-secret-that-is-long-enough-32chars", "http://localhost:8080"); err != nil {
		t.Fatalf("initJWT() error: %v", err)
	}

	now := time.Now()
	claims := UserClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "http://localhost:8080",
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			// Subject intentionally omitted
		},
		Name: "test",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	tokenStr, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	_, err = ValidateUserToken(tokenStr)
	if err == nil {
		t.Error("ValidateUserToken() expected error for missing subject")
	}
}

func TestValidateUserToken_WrongSigningKey(t *testing.T) {
	if err := initJWT("test-secret-that-is-long-enough-32chars", "http://localhost:8080"); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	_, wrongKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	claims := UserClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		Name: "test",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	tokenStr, err := token.SignedString(wrongKey)
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	_, err = ValidateUserToken(tokenStr)
	if err == nil {
		t.Error("ValidateUserToken() expected error for wrong signing key")
	}
}

func TestGenerateAndValidateAgentToken(t *testing.T) {
	if err := initJWT("test-secret-that-is-long-enough-32chars", "http://localhost:8080"); err != nil {
		t.Fatalf("initJWT() error: %v", err)
	}

	agent := &models.Agent{Base: models.Base{Id: "agent-456"}, KeyId: crypto.EncryptedString("key-abc")}
	tokenStr, err := GenerateAgentToken(agent)
	if err != nil {
		t.Fatalf("GenerateAgentToken() error: %v", err)
	}
	if tokenStr == "" {
		t.Fatal("GenerateAgentToken() returned empty token")
	}

	claims, err := ValidateAgentToken(tokenStr)
	if err != nil {
		t.Fatalf("ValidateAgentToken() error: %v", err)
	}
	if claims.Issuer != "http://localhost:8080" {
		t.Errorf("expected Issuer %q, got %q", "http://localhost:8080", claims.Issuer)
	}
	if claims.Subject != "agent-456" {
		t.Errorf("expected Subject %q, got %q", "agent-456", claims.Subject)
	}
	if claims.KeyId != "key-abc" {
		t.Errorf("expected KeyId %q, got %q", "key-abc", claims.KeyId)
	}
	if len(claims.Audience) != 1 || claims.Audience[0] != "agent" {
		t.Errorf("expected Audience [\"agent\"], got %v", claims.Audience)
	}
	if claims.NotBefore == nil {
		t.Error("expected NotBefore to be set")
	}
}

func TestGenerateAndValidateAgentToken_SetsKeyIdWhenMissing(t *testing.T) {
	if err := initJWT("test-secret-that-is-long-enough-32chars", "http://localhost:8080"); err != nil {
		t.Fatalf("initJWT() error: %v", err)
	}

	agent := &models.Agent{Base: models.Base{Id: "agent-789"}}
	tokenStr, err := GenerateAgentToken(agent)
	if err != nil {
		t.Fatalf("GenerateAgentToken() error: %v", err)
	}
	if tokenStr == "" {
		t.Fatal("GenerateAgentToken() returned empty token")
	}
	if agent.KeyId.String() == "" {
		t.Fatal("GenerateAgentToken() expected to set KeyId when missing")
	}

	claims, err := ValidateAgentToken(tokenStr)
	if err != nil {
		t.Fatalf("ValidateAgentToken() error: %v", err)
	}
	if claims.Subject != "agent-789" {
		t.Errorf("expected Subject %q, got %q", "agent-789", claims.Subject)
	}
	if claims.KeyId != agent.KeyId.String() {
		t.Errorf("expected KeyId %q, got %q", agent.KeyId.String(), claims.KeyId)
	}
}

func TestGenerateAgentToken_MissingId(t *testing.T) {
	if err := initJWT("test-secret-that-is-long-enough-32chars", "http://localhost:8080"); err != nil {
		t.Fatalf("initJWT() error: %v", err)
	}

	agent := &models.Agent{KeyId: crypto.EncryptedString("key-abc")}
	_, err := GenerateAgentToken(agent)
	if err == nil {
		t.Error("GenerateAgentToken() expected error for agent with missing Id")
	}
}

func TestValidateAgentToken_Invalid(t *testing.T) {
	if err := initJWT("test-secret-that-is-long-enough-32chars", "http://localhost:8080"); err != nil {
		t.Fatalf("initJWT() error: %v", err)
	}

	_, err := ValidateAgentToken("invalid.token.here")
	if err == nil {
		t.Error("ValidateAgentToken() expected error for invalid token")
	}
}

func TestValidateAgentToken_WrongIssuer(t *testing.T) {
	if err := initJWT("test-secret-that-is-long-enough-32chars", "http://localhost:8080"); err != nil {
		t.Fatalf("initJWT() error: %v", err)
	}

	now := time.Now()
	claims := AgentClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "http://evil.example.com",
			Subject:   "agent-456",
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Audience:  []string{"agent"},
		},
		KeyId: "key-abc",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	tokenStr, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	_, err = ValidateAgentToken(tokenStr)
	if err == nil {
		t.Error("ValidateAgentToken() expected error for wrong issuer")
	}
}

func TestValidateAgentToken_MissingSubject(t *testing.T) {
	if err := initJWT("test-secret-that-is-long-enough-32chars", "http://localhost:8080"); err != nil {
		t.Fatalf("initJWT() error: %v", err)
	}

	now := time.Now()
	claims := AgentClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "http://localhost:8080",
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Audience:  []string{"agent"},
			// Subject intentionally omitted
		},
		KeyId: "key-abc",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	tokenStr, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	_, err = ValidateAgentToken(tokenStr)
	if err == nil {
		t.Error("ValidateAgentToken() expected error for missing subject")
	}
}

func TestValidateAgentToken_WrongSigningKey(t *testing.T) {
	if err := initJWT("test-secret-that-is-long-enough-32chars", "http://localhost:8080"); err != nil {
		t.Fatalf("initJWT() error: %v", err)
	}

	_, wrongKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	now := time.Now()
	claims := AgentClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "http://localhost:8080",
			Subject:   "agent-456",
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Audience:  []string{"agent"},
		},
		KeyId: "key-abc",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	tokenStr, err := token.SignedString(wrongKey)
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	_, err = ValidateAgentToken(tokenStr)
	if err == nil {
		t.Error("ValidateAgentToken() expected error for wrong signing key")
	}
}

func TestValidateAgentToken_WrongAudience(t *testing.T) {
	if err := initJWT("test-secret-that-is-long-enough-32chars", "http://localhost:8080"); err != nil {
		t.Fatalf("initJWT() error: %v", err)
	}

	user := &models.User{Base: models.Base{Id: "user-123"}, Name: "test", Email: "test@example.com"}
	tokenStr, err := GenerateUserToken(user)
	if err != nil {
		t.Fatalf("GenerateUserToken() error: %v", err)
	}

	_, err = ValidateAgentToken(tokenStr)
	if err == nil {
		t.Error("ValidateAgentToken() expected error when validating a user token")
	}
}

func TestValidateUserToken_WrongAudience(t *testing.T) {
	if err := initJWT("test-secret-that-is-long-enough-32chars", "http://localhost:8080"); err != nil {
		t.Fatalf("initJWT() error: %v", err)
	}

	agent := &models.Agent{Base: models.Base{Id: "agent-456"}, KeyId: crypto.EncryptedString("key-abc")}
	tokenStr, err := GenerateAgentToken(agent)
	if err != nil {
		t.Fatalf("GenerateAgentToken() error: %v", err)
	}

	_, err = ValidateUserToken(tokenStr)
	if err == nil {
		t.Error("ValidateUserToken() expected error when validating an agent token")
	}
}
