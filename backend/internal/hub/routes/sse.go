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
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
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

// topicVisible reports whether the user may see updates of the topic.
func topicVisible(userID string, perms *rbac.Permissions, topic sse.Topic) bool {
	if !topic.SentTo(userID) {
		return false
	}
	if topic.AdminOnly() {
		return perms.IsAdmin()
	}
	resource, id := topic.Resource()
	if resource == "" {
		return true
	}
	if id == "" {
		return perms.CanAny(resource, models.ActionRead)
	}
	return perms.Can(resource, models.ActionRead, id)
}

// reloadPermissions picks up role changes of a long-lived stream. On failure the
// previous permissions are kept, the next attempt is at the latest after a keep-alive.
func reloadPermissions(ctx context.Context, userID string, current *rbac.Permissions) *rbac.Permissions {
	perms, err := rbac.Load(ctx, db.DB, userID)
	if err != nil {
		permissionsLog.Warn().Err(err).Str("userId", userID).Msg("failed to reload permissions of event stream")
		return current
	}
	return perms
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

	perms, ok := currentPermissions(c)
	if !ok {
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
			// Role changes publish the profile topic, so filter with the new permissions.
			if event.ReloadsProfileOf(claims.Subject) {
				perms = reloadPermissions(c.Request.Context(), claims.Subject, perms)
			}
			message, visible := event.Message(func(topic sse.Topic) bool { return topicVisible(claims.Subject, perms, topic) })
			if !visible {
				continue
			}
			data, err := json.Marshal(message)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", message.Type, data); err != nil {
				return
			}
			c.Writer.Flush()
		case <-keepAlive.C:
			if sessionRevoked(c.Request.Context(), claims) {
				writeUnauthorizedEvent(c)
				return
			}
			perms = reloadPermissions(c.Request.Context(), claims.Subject, perms)
			if _, err := fmt.Fprint(c.Writer, ": ping\n\n"); err != nil {
				return
			}
			c.Writer.Flush()
		case <-c.Request.Context().Done():
			return
		}
	}
}
