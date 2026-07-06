package middleware

import (
	"context"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const sseRoutePath = "/api/v1/events"

func TimeoutMiddleware(timeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		// WebSocket and SSE connections are long-lived; skip the timeout.
		// The SSE exemption is tied to the registered route path, not the
		// client-supplied Accept header, to prevent bypass via a crafted header.
		if strings.EqualFold(c.Request.Header.Get("Upgrade"), "websocket") ||
			c.FullPath() == sseRoutePath {
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
