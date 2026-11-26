package routes

// The hub is an OAuth 2.1 authorization server for MCP clients, as the MCP
// authorization spec describes. A client registers itself (RFC 7591), the user
// approves it on the consent page of the web UI, and the client gets an API
// token limited to the approved scopes, with a refresh token to replace it.
// PKCE (RFC 7636) is required, the token only works on the MCP endpoint, which
// is the only resource it can be asked for (RFC 8707), and redirects carry the
// issuer (RFC 9207).

import (
	"crypto/hkdf"
	"crypto/sha256"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/middleware"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

const (
	oauthAccessTokenLifetime = time.Hour
	// A grant ends when the client didn't refresh it for this long.
	oauthRefreshTokenLifetime = 30 * 24 * time.Hour
	oauthCodeLifetime         = time.Minute
)

var (
	oauthIssuer    string
	oauthClientKey []byte
	// oauthNow is replaced in tests.
	oauthNow = time.Now
)

// mcpScopes are what the MCP tools need, and all a client can ask for. The
// consent page lets the user drop the ones they don't want.
var mcpScopes = []models.APITokenScope{
	models.NewAPITokenScope(models.ResourceApplication, models.ActionRead),
	models.NewAPITokenScope(models.ResourceAgent, models.ActionRead),
	models.NewAPITokenScope(models.ResourceApplication, models.ActionDeploy),
}

// InitOAuth sets the issuer, which is the app URL, and derives the key that
// signs the client IDs, so they keep working across restarts.
func InitOAuth(appSecret, appURL string) error {
	key, err := hkdf.Key(sha256.New, []byte(appSecret), nil, "OAUTH_CLIENT_ID_KEY", sha256.Size)
	if err != nil {
		return fmt.Errorf("routes.InitOAuth: %w", err)
	}
	oauthIssuer = appURL
	oauthClientKey = key
	return nil
}

func mcpResource() string {
	return oauthIssuer + middleware.MCPPath
}

// MCPAuthChallenge is the WWW-Authenticate header of a 401 from the MCP
// endpoint, which tells clients where to find the authorization server.
func MCPAuthChallenge() string {
	return fmt.Sprintf("Bearer resource_metadata=%q, scope=%q",
		oauthIssuer+"/.well-known/oauth-protected-resource"+middleware.MCPPath, joinScopes(mcpScopes))
}

// OAuthProtectedResourceMetadataHandler serves the metadata of the MCP endpoint
// (RFC 9728), which names the hub as its authorization server.
func OAuthProtectedResourceMetadataHandler() gin.HandlerFunc {
	return gin.WrapH(mcpauth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
		Resource:               mcpResource(),
		AuthorizationServers:   []string{oauthIssuer},
		ScopesSupported:        scopeStrings(mcpScopes),
		BearerMethodsSupported: []string{"header"},
		ResourceName:           "OrcaCD",
	}))
}

type authorizationServerMetadata struct {
	Issuer                                     string   `json:"issuer"`
	AuthorizationEndpoint                      string   `json:"authorization_endpoint"`
	TokenEndpoint                              string   `json:"token_endpoint"`
	RegistrationEndpoint                       string   `json:"registration_endpoint"`
	ScopesSupported                            []string `json:"scopes_supported"`
	ResponseTypesSupported                     []string `json:"response_types_supported"`
	GrantTypesSupported                        []string `json:"grant_types_supported"`
	TokenEndpointAuthMethodsSupported          []string `json:"token_endpoint_auth_methods_supported"`
	CodeChallengeMethodsSupported              []string `json:"code_challenge_methods_supported"`
	AuthorizationResponseIssParameterSupported bool     `json:"authorization_response_iss_parameter_supported"`
	ClientIdMetadataDocumentSupported          bool     `json:"client_id_metadata_document_supported"`
}

// OAuthCORS lets MCP clients that run in a browser use the endpoints of the
// clients. The endpoints don't use the session cookie, so any site may call
// them.
var OAuthCORS = allowAnyOrigin("GET, POST, OPTIONS", "Content-Type, Authorization", "")

// MCPCORS lets MCP clients in a browser call the MCP endpoint with their token,
// and read the challenge that starts the OAuth flow. Requests with the session
// cookie can't use it: the cookie is SameSite=Strict, and ValidateOrigin checks
// requests that send it.
var MCPCORS = allowAnyOrigin("GET, POST, DELETE, OPTIONS",
	"Authorization, Content-Type, Accept, Mcp-Protocol-Version, Mcp-Session-Id, Last-Event-ID",
	"WWW-Authenticate, Mcp-Session-Id")

// allowAnyOrigin allows cross-origin requests without credentials and answers
// preflight requests.
func allowAnyOrigin(methods, headers, exposedHeaders string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", methods)
		c.Header("Access-Control-Allow-Headers", headers)
		if exposedHeaders != "" {
			c.Header("Access-Control-Expose-Headers", exposedHeaders)
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// OAuthAuthorizationServerMetadataHandler serves the metadata of the
// authorization server (RFC 8414). Clients read it after OAuthCORS.
func OAuthAuthorizationServerMetadataHandler(c *gin.Context) {

	c.JSON(http.StatusOK, authorizationServerMetadata{
		Issuer: oauthIssuer,
		// The consent page of the web UI
		AuthorizationEndpoint:                      oauthIssuer + "/oauth/authorize",
		TokenEndpoint:                              oauthIssuer + "/api/v1/oauth/token",
		RegistrationEndpoint:                       oauthIssuer + "/api/v1/oauth/register",
		ScopesSupported:                            scopeStrings(mcpScopes),
		ResponseTypesSupported:                     []string{"code"},
		GrantTypesSupported:                        []string{grantTypeAuthorizationCode, grantTypeRefreshToken},
		TokenEndpointAuthMethodsSupported:          []string{"none"},
		CodeChallengeMethodsSupported:              []string{"S256"},
		AuthorizationResponseIssParameterSupported: true,
		ClientIdMetadataDocumentSupported:          true,
	})
}

// parseOAuthScopes parses the space separated scope parameter. The tokens only
// work on the MCP endpoint, so only the scopes of its tools can be asked for.
// Others, like offline_access, are left out, as the server may grant fewer
// scopes than asked for (RFC 6749 section 3.3). Without a scope, the client
// asks for all of them.
func parseOAuthScopes(raw string) ([]models.APITokenScope, error) {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return mcpScopes, nil
	}
	var scopes []models.APITokenScope
	for _, scope := range mcpScopes {
		if slices.Contains(fields, string(scope)) {
			scopes = append(scopes, scope)
		}
	}
	if len(scopes) == 0 {
		return nil, fmt.Errorf("the scope must include one of %s", joinScopes(mcpScopes))
	}
	return scopes, nil
}

// isOAuthResource reports whether the resource parameter names the hub.
func isOAuthResource(resource string) bool {
	return resource == mcpResource() || resource == oauthIssuer
}

func joinScopes(scopes []models.APITokenScope) string {
	return strings.Join(scopeStrings(scopes), " ")
}

func scopeStrings(scopes []models.APITokenScope) []string {
	result := make([]string, len(scopes))
	for i, scope := range scopes {
		result[i] = string(scope)
	}
	return result
}

// oauthError responds with an error of the token and registration endpoints.
func oauthError(c *gin.Context, status int, code, description string) {
	c.Header("Cache-Control", "no-store")
	c.JSON(status, gin.H{"error": code, "error_description": description})
}
