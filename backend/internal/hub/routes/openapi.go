package routes

import (
	"errors"
	"fmt"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Tags group the operations in the API docs.
const (
	tagSystem        = "System"
	tagAuth          = "Authentication"
	tagProfile       = "Profile"
	tagAPITokens     = "API tokens"
	tagApplications  = "Applications"
	tagRepositories  = "Repositories"
	tagAgents        = "Agents"
	tagNotifications = "Notifications"
	tagWebhooks      = "Webhooks"
	tagAdmin         = "Admin"
)

// idInput is the input of routes that only take the resource from the path.
type idInput struct {
	ID string `path:"id" doc:"The ID of the resource"`
}

// requires documents the permission a route checks, which an API token also
// needs as a scope. handlers.go enforces it.
func requires(resource models.PermissionResource, action models.PermissionAction) string {
	return fmt.Sprintf("Needs the `%s:%s` permission.", resource, action)
}

// requiresAdmin documents routes behind RequireAdmin.
const requiresAdmin = "Needs an admin, or an API token with the `admin` scope of an admin."

func internalError() error {
	return huma.Error500InternalServerError("internal server error")
}

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
