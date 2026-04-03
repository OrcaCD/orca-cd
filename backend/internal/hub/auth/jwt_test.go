package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/golang-jwt/jwt/v5"
)

func TestInit(t *testing.T) {
	if err := initJWT("test-secret-that-is-long-enough-32chars", "http://localhost:8080"); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
}

func TestGenerateAndValidateToken(t *testing.T) {
	if err := initJWT("test-secret-that-is-long-enough-32chars", "http://localhost:8080"); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	user := &models.User{Base: models.Base{Id: "user-123"}, Name: "admin"}
	token, err := GenerateToken(user)
	if err != nil {
		t.Fatalf("GenerateToken() error: %v", err)
	}
	if token == "" {
		t.Fatal("GenerateToken() returned empty token")
	}

	claims, err := ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken() error: %v", err)
	}
	if claims.Issuer != "http://localhost:8080" {
		t.Errorf("expected Issuer %q, got %q", "http://localhost:8080", claims.Issuer)
	}
	if claims.Subject != "user-123" {
		t.Errorf("expected Subject %q, got %q", "user-123", claims.Subject)
	}
	if claims.Name != "admin" {
		t.Errorf("expected Name %q, got %q", "admin", claims.Name)
	}
	if claims.NotBefore == nil {
		t.Error("expected NotBefore to be set")
	}
}

func TestValidateToken_Invalid(t *testing.T) {
	if err := initJWT("test-secret-that-is-long-enough-32chars", "http://localhost:8080"); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	_, err := ValidateToken("invalid.token.here")
	if err == nil {
		t.Error("ValidateToken() expected error for invalid token")
	}
}

func TestValidateToken_Expired(t *testing.T) {
	if err := initJWT("test-secret-that-is-long-enough-32chars", "http://localhost:8080"); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	now := time.Now().Add(-2 * time.Hour)
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(-1 * time.Hour)),
		},
		Name: "admin",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	tokenStr, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatalf("failed to create expired token: %v", err)
	}

	_, err = ValidateToken(tokenStr)
	if err == nil {
		t.Error("ValidateToken() expected error for expired token")
	}
}

func TestValidateToken_WrongIssuer(t *testing.T) {
	if err := initJWT("test-secret-that-is-long-enough-32chars", "http://localhost:8080"); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	now := time.Now()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "http://evil.example.com",
			Subject:   "user-123",
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
		Name: "admin",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	tokenStr, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	_, err = ValidateToken(tokenStr)
	if err == nil {
		t.Error("ValidateToken() expected error for wrong issuer")
	}
}

func TestValidateToken_MissingSubject(t *testing.T) {
	if err := initJWT("test-secret-that-is-long-enough-32chars", "http://localhost:8080"); err != nil {
		t.Fatalf("initJWT() error: %v", err)
	}

	now := time.Now()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "http://localhost:8080",
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			// Subject intentionally omitted
		},
		Name: "admin",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	tokenStr, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	_, err = ValidateToken(tokenStr)
	if err == nil {
		t.Error("ValidateToken() expected error for missing subject")
	}
}

func TestValidateToken_WrongSigningKey(t *testing.T) {
	if err := initJWT("test-secret-that-is-long-enough-32chars", "http://localhost:8080"); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	_, wrongKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		Name: "admin",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	tokenStr, err := token.SignedString(wrongKey)
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	_, err = ValidateToken(tokenStr)
	if err == nil {
		t.Error("ValidateToken() expected error for wrong signing key")
	}
}
