package middleware

import (
	"errors"
	"net/http"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {

		authCookie, err := auth.GetAuthCookie(c)

		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authentication"})
			c.Abort()
			return
		}

		claims, err := auth.ValidateUserToken(authCookie)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			c.Abort()
			return
		}

		// The token alone cannot be revoked, so the user is loaded on every request to reject
		// deleted users and tokens issued before their version was bumped.
		user, err := gorm.G[models.User](db.DB).Where("id = ?", claims.Subject).First(c.Request.Context())
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				auth.ClearAuthCookie(c)
				c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
				c.Abort()
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			c.Abort()
			return
		}

		if user.TokenVersion != claims.TokenVersion {
			auth.ClearAuthCookie(c)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			c.Abort()
			return
		}

		if user.PasswordChangeRequired && !isPasswordChangeAllowedPath(c.Request.URL.Path) {
			c.JSON(http.StatusForbidden, gin.H{"error": "password change required"})
			c.Abort()
			return
		}

		auth.SetClaims(c, claims)
		auth.SetUser(c, &user)
		c.Next()
	}
}

func isPasswordChangeAllowedPath(path string) bool {
	switch path {
	case "/api/v1/auth/profile", "/api/v1/auth/logout", "/api/v1/auth/logout-all", "/api/v1/auth/change-password":
		return true
	default:
		return false
	}
}
