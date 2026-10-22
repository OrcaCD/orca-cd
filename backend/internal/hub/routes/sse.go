package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const maxSSEConnectionsPerUser = 10

// sseKeepAliveInterval also bounds how long a stream outlives a deleted user or revoked token.
var sseKeepAliveInterval = 30 * time.Second

var (
	sseConnsMu sync.Mutex
	sseConns   = make(map[string]int)
)

func acquireSSESlot(userID string) bool {
	sseConnsMu.Lock()
	defer sseConnsMu.Unlock()
	if sseConns[userID] >= maxSSEConnectionsPerUser {
		return false
	}
	sseConns[userID]++
	return true
}

func releaseSSESlot(userID string) {
	sseConnsMu.Lock()
	defer sseConnsMu.Unlock()
	if sseConns[userID] <= 1 {
		delete(sseConns, userID)
		return
	}
	sseConns[userID]--
}

// sessionRevoked reports whether the user of a stream was deleted or their tokens were revoked.
// Database errors keep the stream open, the next keep-alive checks again.
func sessionRevoked(ctx context.Context, claims *auth.UserClaims) bool {
	user, err := gorm.G[models.User](db.DB).Where("id = ?", claims.Subject).First(ctx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true
	}
	if err != nil {
		return false
	}
	return user.TokenVersion != claims.TokenVersion
}

func writeUnauthorizedEvent(c *gin.Context) {
	if _, err := fmt.Fprint(c.Writer, "event: unauthorized\ndata: {}\n\n"); err != nil {
		return
	}
	c.Writer.Flush()
}

func SSEHandler(c *gin.Context) {
	claims, ok := auth.GetClaims(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authentication"})
		return
	}

	if !acquireSSESlot(claims.Subject) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many open event streams"})
		return
	}
	defer releaseSSESlot(claims.Subject)

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

	keepAlive := time.NewTicker(sseKeepAliveInterval)
	defer keepAlive.Stop()

	for {
		select {
		case <-timer.C:
			writeUnauthorizedEvent(c)
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
			if sessionRevoked(c.Request.Context(), claims) {
				writeUnauthorizedEvent(c)
				return
			}
			if _, err := fmt.Fprint(c.Writer, ": ping\n\n"); err != nil {
				return
			}
			c.Writer.Flush()
		case <-c.Request.Context().Done():
			return
		}
	}
}
