package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/gin-gonic/gin"
)

func TestRequireAuth_ValidToken(t *testing.T) {
	if err := auth.Init("test-secret-that-is-long-enough-32chars", "http://localhost:8080"); err != nil {
		t.Fatalf("auth.Init() error: %v", err)
	}

	token, err := auth.GenerateToken("user-123", "admin")
	if err != nil {
		t.Fatalf("GenerateToken() error: %v", err)
	}

	router := gin.New()
	router.GET("/protected", RequireAuth(), func(c *gin.Context) {
		userID := c.GetString(UserIDKey)
		c.JSON(http.StatusOK, gin.H{"userID": userID})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRequireAuth_MissingHeader(t *testing.T) {
	router := gin.New()
	router.GET("/protected", RequireAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestRequireAuth_InvalidFormat(t *testing.T) {
	router := gin.New()
	router.GET("/protected", RequireAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestRequireAuth_InvalidToken(t *testing.T) {
	if err := auth.Init("test-secret-that-is-long-enough-32chars", "http://localhost:8080"); err != nil {
		t.Fatalf("auth.Init() error: %v", err)
	}

	router := gin.New()
	router.GET("/protected", RequireAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer invalid.token.here")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}
