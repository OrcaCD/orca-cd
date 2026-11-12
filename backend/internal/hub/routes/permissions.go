package routes

import (
	"context"
	"errors"
	"net/http"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/OrcaCD/orca-cd/internal/shared/logger"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
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

// canDeleteApplicationsOf checks that the user may delete every application
// that is deleted together with its repository or agent.
func canDeleteApplicationsOf(c *gin.Context, column, id string) bool {
	return canOnApplicationsOf(c, models.ActionDelete, column, id, "insufficient permissions to delete its applications")
}

// canOnApplicationsOf checks that the action is allowed on every application
// of a repository or agent, including the ones the user can't see.
func canOnApplicationsOf(c *gin.Context, action models.PermissionAction, column, id, message string) bool {
	perms, ok := currentPermissions(c)
	if !ok {
		return false
	}
	if perms.CanAll(models.ResourceApplication, action) {
		return true
	}

	apps, err := gorm.G[models.Application](db.DB).Select("id").Where(column+" = ?", id).Find(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return false
	}
	for i := range apps {
		if !perms.Can(models.ResourceApplication, action, apps[i].Id) {
			respondForbidden(c, message)
			return false
		}
	}
	return true
}

func respondForbidden(c *gin.Context, message string) {
	c.JSON(http.StatusForbidden, gin.H{"error": message})
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
