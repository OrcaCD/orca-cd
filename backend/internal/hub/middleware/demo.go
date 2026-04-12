package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func DemoBlocking() gin.HandlerFunc {
	return func(c *gin.Context) {
		if isDemoAllowedPath(c.Request.URL.Path) {
			c.Next()
			return
		}

		if c.Request.Method != http.MethodGet {
			c.String(http.StatusForbidden, "This action is not allowed in demo mode.")
			c.Abort()
			return
		}

		c.Next()
	}
}

func isDemoAllowedPath(path string) bool {
	switch path {
	case "/api/v1/auth/login", "/api/v1/auth/logout":
		return true
	default:
		return false
	}
}
