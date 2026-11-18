package auth

import (
	"net/http/httptest"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
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
		Subject: "user-123",
	}
	SetClaims(c, testClaims)

	// Get claims
	retrievedClaims, exists := GetClaims(c)
	if !exists {
		t.Error("expected exists=true after setting claims")
	}
	if retrievedClaims == nil {
		t.Fatal("expected claims to be non-nil after setting")
	}
	if retrievedClaims.Subject != "user-123" {
		t.Errorf("expected Subject %q, got %q", "user-123", retrievedClaims.Subject)
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

func TestSetUser_And_GetUser(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	if user, exists := GetUser(c); exists || user != nil {
		t.Error("expected no user before SetUser")
	}

	SetUser(c, &models.User{Id: "user-123"})

	user, exists := GetUser(c)
	if !exists || user == nil {
		t.Fatal("expected user after SetUser")
	}
	if user.Id != "user-123" {
		t.Errorf("unexpected user %+v", user)
	}

	c.Set(userContextKey, "invalid-value")
	if user, exists := GetUser(c); exists || user != nil {
		t.Error("expected no user when context has wrong type")
	}
}

func TestSetAPIToken_And_GetAPIToken(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	if _, ok := GetAPIToken(c); ok {
		t.Error("expected no API token before it is set")
	}

	SetAPIToken(c, &models.APIToken{Name: "CI"})

	token, ok := GetAPIToken(c)
	if !ok || token.Name != "CI" {
		t.Errorf("GetAPIToken() = (%v, %v), want the token that was set", token, ok)
	}
}

func TestGetBearerToken(t *testing.T) {
	tests := []struct {
		name      string
		header    string
		wantToken string
		wantOk    bool
	}{
		{"bearer", "Bearer orca_abc", "orca_abc", true},
		{"lowercase scheme", "bearer orca_abc", "orca_abc", true},
		{"extra spaces", "Bearer   orca_abc ", "orca_abc", true},
		{"missing", "", "", false},
		{"basic auth", "Basic dXNlcjpwYXNz", "", false},
		{"scheme only", "Bearer", "", false},
		{"empty token", "Bearer  ", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("GET", "/", nil)
			if tt.header != "" {
				c.Request.Header.Set("Authorization", tt.header)
			}

			token, ok := GetBearerToken(c)
			if token != tt.wantToken || ok != tt.wantOk {
				t.Errorf("GetBearerToken() = (%q, %v), want (%q, %v)", token, ok, tt.wantToken, tt.wantOk)
			}
		})
	}
}
