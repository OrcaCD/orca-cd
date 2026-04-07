package middleware

import (
	"net/http"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/gin-gonic/gin"
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

		if claims.PasswordChangeRequired && !isPasswordChangeAllowedPath(c.Request.URL.Path) {
			c.JSON(http.StatusForbidden, gin.H{"error": "password change required"})
			c.Abort()
			return
		}

		auth.SetClaims(c, claims)
		c.Next()
	}
}

func isPasswordChangeAllowedPath(path string) bool {
	switch path {
	case "/api/v1/auth/profile", "/api/v1/auth/logout", "/api/v1/auth/change-password":
		return true
	default:
		return false
	}
}
