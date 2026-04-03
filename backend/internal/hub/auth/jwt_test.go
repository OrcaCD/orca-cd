package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestInit(t *testing.T) {
	if err := Init("test-secret-that-is-long-enough-32chars"); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
}

func TestGenerateAndValidateToken(t *testing.T) {
	if err := Init("test-secret-that-is-long-enough-32chars"); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	token, err := GenerateToken("user-123", "admin")
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
	if claims.UserID != "user-123" {
		t.Errorf("expected UserID %q, got %q", "user-123", claims.UserID)
	}
	if claims.Username != "admin" {
		t.Errorf("expected Username %q, got %q", "admin", claims.Username)
	}
}

func TestValidateToken_Invalid(t *testing.T) {
	if err := Init("test-secret-that-is-long-enough-32chars"); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	_, err := ValidateToken("invalid.token.here")
	if err == nil {
		t.Error("ValidateToken() expected error for invalid token")
	}
}

func TestValidateToken_Expired(t *testing.T) {
	if err := Init("test-secret-that-is-long-enough-32chars"); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	now := time.Now().Add(-2 * time.Hour)
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(-1 * time.Hour)),
		},
		UserID:   "user-123",
		Username: "admin",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString(signingKey)
	if err != nil {
		t.Fatalf("failed to create expired token: %v", err)
	}

	_, err = ValidateToken(tokenStr)
	if err == nil {
		t.Error("ValidateToken() expected error for expired token")
	}
}

func TestValidateToken_WrongSigningKey(t *testing.T) {
	if err := Init("test-secret-that-is-long-enough-32chars"); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		UserID:   "user-123",
		Username: "admin",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString([]byte("wrong-key-that-is-32-characters!"))
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	_, err = ValidateToken(tokenStr)
	if err == nil {
		t.Error("ValidateToken() expected error for wrong signing key")
	}
}
