package routes

import (
	"context"
	"errors"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/OrcaCD/orca-cd/internal/shared/logger"
	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var permissionsLog = logger.New("permissions", false)

// permissionsFor returns the permissions of the authenticated user.
func permissionsFor(c *gin.Context) (*rbac.Permissions, error) {
	perms, err := rbac.For(c)
	if err != nil {
		if errors.Is(err, rbac.ErrMissingUser) {
			return nil, huma.Error401Unauthorized("missing authentication")
		}
		return nil, internalError()
	}
	return perms, nil
}

// checkDeleteApplicationsOf checks that the user may delete every application
// that is deleted together with its repository or agent.
func checkDeleteApplicationsOf(c *gin.Context, column, id string) error {
	return checkApplicationsOf(c, models.ActionDelete, column, id, "insufficient permissions to delete its applications")
}

// checkApplicationsOf checks that the action is allowed on every application of
// a repository or agent, including the ones the user can't see.
func checkApplicationsOf(c *gin.Context, action models.PermissionAction, column, id, message string) error {
	perms, err := permissionsFor(c)
	if err != nil {
		return err
	}
	if perms.CanAll(models.ResourceApplication, action) {
		return nil
	}

	apps, err := gorm.G[models.Application](db.DB).Select("id").Where(column+" = ?", id).Find(c.Request.Context())
	if err != nil {
		return internalError()
	}
	for i := range apps {
		if !perms.Can(models.ResourceApplication, action, apps[i].Id) {
			return huma.Error403Forbidden(message)
		}
	}
	return nil
}

// currentPermissions is permissionsFor for the plain Gin handlers, which
// respond with the error themselves.
func currentPermissions(c *gin.Context) (*rbac.Permissions, bool) {
	perms, err := permissionsFor(c)
	if err != nil {
		if statusErr, ok := errors.AsType[huma.StatusError](err); ok {
			c.JSON(statusErr.GetStatus(), gin.H{"error": statusErr.Error()})
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
