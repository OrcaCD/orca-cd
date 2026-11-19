package routes

import (
	"fmt"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/danielgtaylor/huma/v2"
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
