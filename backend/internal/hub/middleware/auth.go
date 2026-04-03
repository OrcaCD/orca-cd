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

		claims, err := auth.ValidateToken(authCookie)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			c.Abort()
			return
		}

		auth.SetClaims(c, claims)
		c.Next()
	}
}
