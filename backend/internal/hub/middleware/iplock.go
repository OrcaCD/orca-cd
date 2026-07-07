package middleware

import (
	"net/http"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/shared/iplist"
	"github.com/gin-gonic/gin"
)

var ipLockExemptPrefixes = []string{
	"/api/v1/webhooks/",
	"/api/v1/github-actions",
}

var ipLockExemptPaths = map[string]bool{
	"/api/v1/health": true,
	"/api/v1/ws":     true,
}

// IPLock returns a middleware that rejects requests from clients whose IP is
// not in allowedIPs (single addresses or CIDR ranges), except for exempt
// paths (health check, webhooks, agent WebSocket). If allowedIPs contains no
// valid entries, the middleware is a no-op.
func IPLock(allowedIPs []string) gin.HandlerFunc {
	list := iplist.Parse(allowedIPs)
	if list.Empty() {
		return func(c *gin.Context) { c.Next() }
	}

	return func(c *gin.Context) {
		if isIPLockExempt(c.Request.URL.Path) {
			c.Next()
			return
		}

		if !list.Contains(c.ClientIP()) {
			c.String(http.StatusForbidden, "403 forbidden: ip not allowed")
			c.Abort()
			return
		}

		c.Next()
	}
}

func isIPLockExempt(path string) bool {
	if ipLockExemptPaths[path] {
		return true
	}
	for _, prefix := range ipLockExemptPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}
