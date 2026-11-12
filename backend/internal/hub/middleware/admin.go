package middleware

import (
	"errors"
	"net/http"

	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/gin-gonic/gin"
)

func RequireAdmin() gin.HandlerFunc {
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

		if !perms.IsAdmin() {
			c.JSON(http.StatusForbidden, gin.H{"error": "admin access required"})
			c.Abort()
			return
		}

		c.Next()
	}
}
