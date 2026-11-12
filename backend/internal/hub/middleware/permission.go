package middleware

import (
	"errors"
	"net/http"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/gin-gonic/gin"
)

// RequirePermission rejects requests the user's roles don't allow. The resource is
// taken from the :id route parameter. Routes without it, like creating a
// resource, need the permission on all resources of the type.
func RequirePermission(resource models.PermissionResource, action models.PermissionAction) gin.HandlerFunc {
	return func(c *gin.Context) {
		perms, err := rbac.For(c)
		if err != nil {
			if errors.Is(err, rbac.ErrMissingUser) {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authentication"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			}
			c.Abort()
			return
		}

		if !perms.Can(resource, action, c.Param("id")) {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			c.Abort()
			return
		}

		c.Next()
	}
}
