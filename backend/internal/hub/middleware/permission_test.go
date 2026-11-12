package middleware

import (
	"net/http"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestRequirePermission(t *testing.T) {
	user := &models.User{Id: "user-1", Name: "Deployer", Email: "deployer@example.com"}
	setupAuthTest(t, user)
	token := generateToken(t, user)

	role := models.Role{Name: "Deployers"}
	if err := gorm.G[models.Role](db.DB).Create(t.Context(), &role); err != nil {
		t.Fatalf("failed to create role: %v", err)
	}
	appID := "app-1"
	if err := gorm.G[models.RolePermission](db.DB).Create(t.Context(), &models.RolePermission{
		RoleId: role.Id, Resource: models.ResourceApplication, Action: models.ActionDeploy, ResourceId: &appID,
	}); err != nil {
		t.Fatalf("failed to create permission: %v", err)
	}
	assignRole(t, user.Id, role.Id)

	router := gin.New()
	ok := func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{}) }
	router.POST("/applications/:id/deploy", RequireAuth(), RequirePermission(models.ResourceApplication, models.ActionDeploy), ok)
	router.DELETE("/applications/:id", RequireAuth(), RequirePermission(models.ResourceApplication, models.ActionDelete), ok)
	router.POST("/applications", RequireAuth(), RequirePermission(models.ResourceApplication, models.ActionCreate), ok)
	router.GET("/applications/:id", RequireAuth(), RequirePermission(models.ResourceApplication, models.ActionRead), ok)

	tests := []struct {
		method, path string
		want         int
	}{
		{http.MethodPost, "/applications/app-1/deploy", http.StatusOK},
		{http.MethodGet, "/applications/app-1", http.StatusOK},
		{http.MethodPost, "/applications/app-2/deploy", http.StatusForbidden},
		{http.MethodDelete, "/applications/app-1", http.StatusForbidden},
		{http.MethodPost, "/applications", http.StatusForbidden},
	}
	for _, tt := range tests {
		if w := serveWithToken(router, tt.method, tt.path, token); w.Code != tt.want {
			t.Errorf("%s %s: expected %d, got %d: %s", tt.method, tt.path, tt.want, w.Code, w.Body.String())
		}
	}

	assignRole(t, user.Id, rbac.AdminRoleID)
	if w := serveWithToken(router, http.MethodDelete, "/applications/app-1", token); w.Code != http.StatusOK {
		t.Errorf("expected admins to be allowed everything, got %d", w.Code)
	}
}
