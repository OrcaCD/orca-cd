package middleware

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var streamingRoutePaths = []string{
	"/api/v1/events",
	"/api/v1/applications/:id/containers/:containerId/logs/stream",
	"/api/v1/applications/:id/containers/:containerId/logs/download",
	"/api/v1/applications/:id/containers/:containerId/stats/stream",
}

func StreamingRoutePaths() []string {
	return slices.Clone(streamingRoutePaths)
}

func TimeoutMiddleware(timeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		// WebSocket and streaming connections are long-lived; skip the timeout.
		if strings.EqualFold(c.Request.Header.Get("Upgrade"), "websocket") ||
			slices.Contains(streamingRoutePaths, c.FullPath()) {
			c.Next()
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()

		// Replace the request with one that carries the new context.
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
