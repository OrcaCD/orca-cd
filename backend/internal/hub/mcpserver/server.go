// Package mcpserver serves the Model Context Protocol endpoint of the hub, so
// AI agents can read the state of applications and deploy them. It runs behind
// the same authentication as the REST API: the permissions of the user, limited
// by the scopes of their API token, decide which tools are listed and which
// applications they can see.
package mcpserver

import (
	"context"
	"errors"
	"net/http"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/OrcaCD/orca-cd/internal/shared/logger"
	"github.com/OrcaCD/orca-cd/internal/version"
	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var Log = logger.New("mcp", false)

// errInternal hides database errors from the client, they are logged instead.
var errInternal = errors.New("internal server error")

const instructions = `OrcaCD deploys Docker Compose applications from Git repositories to agents running on Docker hosts.

- An application is a compose file at a path and branch of a repository, deployed by one agent.
- syncStatus tells whether the application runs the compose file of its commit ("synced"), differs from it ("out_of_sync"), is being deployed ("syncing") or is unknown.
- healthStatus is the health of its containers: "healthy", "degraded", "unhealthy", "stopped" or "unknown".
- When a deploy fails, get_application returns the error and the recent events of the application.
- Changes to an application belong in its Git repository, OrcaCD deploys what is committed there.

To deploy an application, call preview_deploy first and show the user the diff and the warnings. Only call deploy_application with its previewId once the user agreed. The deploy is refused when the application changed since the preview.`

type callerKey struct{}

// caller is who calls the tools. Tools check the permissions themselves, as
// the endpoint can't know which application a call is about.
type caller struct {
	user  *models.User
	perms *rbac.Permissions
}

// Handler serves MCP over streamable HTTP. It must run after
// middleware.RequireAuth.
func Handler() gin.HandlerFunc {
	handler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		c, ok := r.Context().Value(callerKey{}).(*caller)
		if !ok {
			return nil
		}
		return newServer(c)
	}, &mcp.StreamableHTTPOptions{
		// Every request carries its token, so no session is kept between them.
		Stateless:    true,
		JSONResponse: true,
		// The protection rejects requests to a localhost address with another
		// Host header, which is what a reverse proxy in front of the hub sends.
		// Requests need an API token or pass ValidateOrigin, which a DNS
		// rebinding page can't do.
		DisableLocalhostProtection: true,
	})

	return func(c *gin.Context) {
		user, ok := auth.GetUser(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authentication"})
			return
		}
		perms, err := rbac.For(c)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			return
		}

		ctx := context.WithValue(c.Request.Context(), callerKey{}, &caller{user: user, perms: perms})
		handler.ServeHTTP(c.Writer, c.Request.WithContext(ctx))
	}
}

// newServer builds a server with the tools the caller is allowed to use, so
// clients don't offer tools that always fail.
func newServer(c *caller) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "orcacd",
		Title:   "OrcaCD",
		Version: version.Version,
	}, &mcp.ServerOptions{Instructions: instructions})

	if c.perms.CanAny(models.ResourceApplication, models.ActionRead) {
		mcp.AddTool(server, listApplicationsTool, c.listApplications)
		mcp.AddTool(server, getApplicationTool, c.getApplication)
	}
	if c.perms.CanAny(models.ResourceAgent, models.ActionRead) {
		mcp.AddTool(server, listAgentsTool, c.listAgents)
	}
	if c.perms.CanAny(models.ResourceApplication, models.ActionDeploy) {
		mcp.AddTool(server, previewDeployTool, c.previewDeploy)
		mcp.AddTool(server, deployApplicationTool, c.deployApplication)
	}
	return server
}
