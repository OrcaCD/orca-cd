package auth

import (
        "net/http/httptest"
        "testing"

        "github.com/gin-gonic/gin"
        "github.com/golang-jwt/jwt/v5"
)

func TestSetClaims_And_GetClaims(t *testing.T) {
        w := httptest.NewRecorder()
        c, _ := gin.CreateTestContext(w)

        // Test GetClaims when no claims are set
        claims, exists := GetClaims(c)
        if exists {
                t.Error("expected exists=false when no claims set")
        }
        if claims != nil {
                t.Error("expected claims=nil when no claims set")
        }

        // Set claims
        testClaims := &UserClaims{
                RegisteredClaims: jwt.RegisteredClaims{
                        Subject: "user-123",
                },
                IsLocal: true,
        }
        SetClaims(c, testClaims)

        // Get claims
        retrievedClaims, exists := GetClaims(c)
        if !exists {
                t.Error("expected exists=true after setting claims")
        }
        if retrievedClaims == nil {
                t.Error("expected claims to be non-nil after setting")
        }
        if retrievedClaims.Subject != "user-123" {
                t.Errorf("expected Subject %q, got %q", "user-123", retrievedClaims.Subject)
        }
        if !retrievedClaims.IsLocal {
                t.Error("expected IsLocal=true")
        }
}

func TestSetClaims_And_GetClaims_InvalidType(t *testing.T) {
        w := httptest.NewRecorder()
        c, _ := gin.CreateTestContext(w)

        // Manually set non-UserClaims value in the context
        c.Set(claimsContextKey, "invalid-value")

        // GetClaims should return false when the value is not a *UserClaims
        claims, exists := GetClaims(c)
        if exists {
                t.Error("expected exists=false when context has wrong type")
        }
        if claims != nil {
                t.Error("expected claims=nil when context has wrong type")
        }
}
