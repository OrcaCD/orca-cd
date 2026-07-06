package routes

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/gin-gonic/gin"
)

func SSEHandler(c *gin.Context) {
	claims, ok := auth.GetClaims(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authentication"})
		return
	}

	// Clear the write deadline so the server's WriteTimeout doesn't kill long-lived SSE connections.
	// Ignore errors: some ResponseWriter implementations (e.g. httptest.ResponseRecorder) don't support deadlines.
	_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Time{})

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	connID, ch := sse.DefaultBroker.Subscribe()
	defer sse.DefaultBroker.Unsubscribe(connID)

	// Flush initial comment to establish the stream
	if _, err := fmt.Fprint(c.Writer, ": connected\n\n"); err != nil {
		return
	}
	c.Writer.Flush()

	timer := time.NewTimer(time.Until(claims.ExpiresAt.Time))
	defer timer.Stop()

	keepAlive := time.NewTicker(30 * time.Second)
	defer keepAlive.Stop()

	for {
		select {
		case <-timer.C:
			if _, err := fmt.Fprint(c.Writer, "event: unauthorized\ndata: {}\n\n"); err != nil {
				return
			}
			c.Writer.Flush()
			return
		case event, open := <-ch:
			if !open {
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event.Type, data); err != nil {
				return
			}
			c.Writer.Flush()
		case <-keepAlive.C:
			if _, err := fmt.Fprint(c.Writer, ": ping\n\n"); err != nil {
				return
			}
			c.Writer.Flush()
		case <-c.Request.Context().Done():
			return
		}
	}
}
