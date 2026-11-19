package routes

import (
	"context"
	"errors"
	"net/http"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/OrcaCD/orca-cd/internal/shared/logger"
	"github.com/gin-gonic/gin"
)

var permissionsLog = logger.New("permissions", false)

// currentPermissions returns the permissions of the authenticated user and
// responds with an error when they cannot be loaded.
func currentPermissions(c *gin.Context) (*rbac.Permissions, bool) {
	perms, err := rbac.For(c)
	if err != nil {
		if errors.Is(err, rbac.ErrMissingUser) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authentication"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return nil, false
	}
	return perms, true
}

// pruneGrants removes the grants on deleted resources and tells the members of
// the roles that lost grants. A failure leaves grants that match nothing, so it
// is only logged.
func pruneGrants(ctx context.Context) {
	roleIds, err := rbac.PruneGrants(ctx, db.DB)
	if err != nil {
		permissionsLog.Error().Err(err).Msg("failed to remove grants on deleted resources")
	}
	if len(roleIds) == 0 {
		return
	}
	members, err := rbac.MemberIDs(ctx, db.DB, roleIds)
	if err != nil {
		permissionsLog.Warn().Err(err).Msg("failed to load the members of roles that lost grants")
	}
	topics := []sse.Topic{sse.TopicAdminRoles}
	for _, id := range members {
		topics = append(topics, sse.TopicProfileOf(id))
	}
	sse.PublishUpdate(topics...)
}
