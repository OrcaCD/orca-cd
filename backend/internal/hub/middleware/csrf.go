package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

var safeMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodOptions: true,
}

func ValidateOrigin(appURL string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if safeMethods[c.Request.Method] {
			c.Next()
			return
		}

		origin := strings.TrimSuffix(c.GetHeader("Origin"), "/")
		if origin == "" {
			c.String(http.StatusForbidden, "403 forbidden: missing origin header")
			c.Abort()
			return
		}

		if origin != appURL {
			c.String(http.StatusForbidden, "403 forbidden: invalid origin")
			c.Abort()
			return
		}

		c.Next()
	}
}
