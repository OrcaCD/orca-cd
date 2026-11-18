package middleware

import (
	"net/http"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/hub/apitoken"
	"github.com/OrcaCD/orca-cd/internal/hub/auth"
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

		// External webhook providers (GitHub, GitLab, Gitea) do not send a
		// matching Origin header, so skip CSRF validation for webhook routes.
		if strings.HasPrefix(c.Request.URL.Path, "/api/v1/webhooks/") {
			c.Next()
			return
		}

		// GitHub Actions sends requests without a browser Origin header.
		if strings.HasPrefix(c.Request.URL.Path, "/api/v1/github-actions") {
			c.Next()
			return
		}

		// API clients send their token in the Authorization header, which a
		// browser never adds to a cross-site request on its own. RequireAuth then
		// ignores the session cookie, so the request can't act with it. Requests
		// with the cookie are checked anyway.
		if token, ok := auth.GetBearerToken(c); ok && apitoken.IsWellFormed(token) {
			if _, err := auth.GetAuthCookie(c); err != nil {
				c.Next()
				return
			}
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
