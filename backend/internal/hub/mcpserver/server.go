// Package mcpserver serves the Model Context Protocol endpoint of the hub, so
// AI agents can read the state of applications and deploy them. It runs behind
// the same authentication as the REST API: the permissions of the user, limited
// by the scopes of their API token, decide which tools are listed and which
// applications they can see.
package mcpserver

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

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

// toolTimeout bounds a tool call. The SDK doesn't pass on the deadline or
// cancellation of the request, so TimeoutMiddleware doesn't apply to tools.
// It is below the 30s of the middleware, so the answer is written before the
// server's write timeout.
const toolTimeout = 25 * time.Second

// maxRequestBytes limits a request, the tools only take a few IDs.
const maxRequestBytes = 64 << 10

// schemaCache keeps the tool schemas, which would otherwise be built by
// reflection for every request, as each request gets its own server.
var schemaCache = mcp.NewSchemaCache()

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
	// request is the request the tools run in, for the security log. The
	// stateless server answers within the request, so it outlives the calls.
	request *gin.Context

	// The SDK asks for the server more than once per request.
	serverOnce sync.Once
	server     *mcp.Server
}

// Handler serves MCP over streamable HTTP. It must run after
// middleware.RequireAuth.
func Handler() gin.HandlerFunc {
	handler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		c, ok := r.Context().Value(callerKey{}).(*caller)
		if !ok {
			return nil
		}
		c.serverOnce.Do(func() { c.server = newServer(c) })
		return c.server
	}, &mcp.StreamableHTTPOptions{
		// Every request carries its token, so no session is kept between them.
		Stateless:    true,
		JSONResponse: true,
		// The protection rejects requests to a localhost address with another
		// Host header, which is what a reverse proxy in front of the hub sends.
		// Requests need an API token or pass ValidateOrigin, which a DNS
		// rebinding page can't do.
		DisableLocalhostProtection: true,
		MaxRequestBodyBytes:        maxRequestBytes,
	})

	return func(c *gin.Context) {
		// The stateless server has no stream to open with GET and no session to
		// end with DELETE. Clients expect a 405 for both.
		if c.Request.Method != http.MethodPost {
			c.Header("Allow", http.MethodPost)
			c.JSON(http.StatusMethodNotAllowed, gin.H{"error": "method not allowed"})
			return
		}

		// The SDK runs every call of a JSON-RPC batch at once, so one request could
		// start thousands of them. Batches were dropped from MCP in 2025-06-18.
		body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxRequestBytes))
		if err != nil {
			if _, tooLarge := errors.AsType[*http.MaxBytesError](err); tooLarge {
				c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
			return
		}
		if bytes.HasPrefix(bytes.TrimLeft(body, " \t\r\n"), []byte("[")) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "batch requests are not supported"})
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))

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

		ctx := context.WithValue(c.Request.Context(), callerKey{}, &caller{user: user, perms: perms, request: c})
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
	}, &mcp.ServerOptions{Instructions: instructions, SchemaCache: schemaCache})

	if c.perms.CanAny(models.ResourceApplication, models.ActionRead) {
		mcp.AddTool(server, listApplicationsTool, withTimeout(c.listApplications))
		mcp.AddTool(server, getApplicationTool, withTimeout(c.getApplication))
	}
	if c.perms.CanAny(models.ResourceAgent, models.ActionRead) {
		mcp.AddTool(server, listAgentsTool, withTimeout(c.listAgents))
	}
	if c.perms.CanAny(models.ResourceApplication, models.ActionDeploy) {
		mcp.AddTool(server, previewDeployTool, withTimeout(c.previewDeploy))
		mcp.AddTool(server, deployApplicationTool, withTimeout(c.deployApplication))
	}
	return server
}

func withTimeout[In, Out any](handler mcp.ToolHandlerFor[In, Out]) mcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		ctx, cancel := context.WithTimeout(ctx, toolTimeout)
		defer cancel()
		return handler(ctx, req, in)
	}
}
