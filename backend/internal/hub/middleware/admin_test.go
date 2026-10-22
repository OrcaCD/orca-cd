package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestRequireAdmin_AdminRole(t *testing.T) {
	user := &models.User{
		Id:    "user-1",
		Name:  "Admin",
		Email: "admin@example.com",
		Role:  models.UserRoleAdmin,
	}
	setupAuthTest(t, user)
	token := generateToken(t, user)

	router := gin.New()
	router.GET("/admin", RequireAuth(), RequireAdmin(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := serveWithToken(router, http.MethodGet, "/admin", token)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRequireAdmin_UserRole(t *testing.T) {
	user := &models.User{
		Id:    "user-2",
		Name:  "Regular",
		Email: "regular@example.com",
		Role:  models.UserRoleUser,
	}
	setupAuthTest(t, user)
	token := generateToken(t, user)

	router := gin.New()
	router.GET("/admin", RequireAuth(), RequireAdmin(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := serveWithToken(router, http.MethodGet, "/admin", token)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRequireAdmin_DemotedAdminRejected(t *testing.T) {
	user := &models.User{
		Id:    "user-3",
		Name:  "Demoted",
		Email: "demoted@example.com",
		Role:  models.UserRoleAdmin,
	}
	setupAuthTest(t, user)
	token := generateToken(t, user)

	if _, err := gorm.G[models.User](db.DB).Where("id = ?", user.Id).Update(t.Context(), "role", models.UserRoleUser); err != nil {
		t.Fatalf("failed to demote user: %v", err)
	}

	router := gin.New()
	router.GET("/admin", RequireAuth(), RequireAdmin(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := serveWithToken(router, http.MethodGet, "/admin", token)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRequireAdmin_NoClaims(t *testing.T) {
	router := gin.New()
	// Skip RequireAuth — no claims set in context
	router.GET("/admin", RequireAdmin(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}
