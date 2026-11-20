package middleware

import (
	"errors"
	"net/http"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/apitoken"
	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/OrcaCD/orca-cd/internal/hub/seclog"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

// MCPPath is the only route the tokens of OAuth clients work on.
const MCPPath = "/api/v1/mcp"

// apiTokenUsedInterval is how often last_used_at of an API token is written at
// most, so busy clients don't cause a write on every request.
const apiTokenUsedInterval = time.Minute

// RequireAuth accepts the session cookie of the web UI or an API token in the
// Authorization header. With a token, the cookie is ignored, since ValidateOrigin
// lets such requests through without checking where they come from.
func RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if token, ok := bearerAPIToken(c); ok {
			authenticateAPIToken(c, token)
			return
		}

		authCookie, err := auth.GetAuthCookie(c)

		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authentication"})
			c.Abort()
			return
		}

		claims, err := auth.ValidateUserToken(authCookie)
		if err != nil {
			if !errors.Is(err, jwt.ErrTokenExpired) {
				seclog.Warn(c, seclog.TokenRejected, "reason", "invalid_token")
			}
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			c.Abort()
			return
		}

		// The token alone cannot be revoked, so the user is loaded on every request to reject
		// deleted users and tokens issued before their version was bumped.
		user, err := gorm.G[models.User](db.DB).Where("id = ?", claims.Subject).First(c.Request.Context())
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				seclog.Warn(c, seclog.TokenRejected, "reason", "unknown_user", "user_id", claims.Subject)
				auth.ClearAuthCookie(c)
				c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
				c.Abort()
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			c.Abort()
			return
		}

		if user.TokenVersion != claims.TokenVersion {
			seclog.Warn(c, seclog.TokenRejected, "reason", "revoked_token", "user_id", user.Id)
			auth.ClearAuthCookie(c)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			c.Abort()
			return
		}

		auth.SetClaims(c, claims)
		auth.SetUser(c, &user)
		c.Next()
	}
}

// bearerAPIToken returns the API token of an "Authorization: Bearer" header.
// Other bearer tokens, like one a reverse proxy adds, are not API tokens and
// don't keep the session cookie from being used.
func bearerAPIToken(c *gin.Context) (string, bool) {
	token, ok := auth.GetBearerToken(c)
	if !ok || !apitoken.IsWellFormed(token) {
		return "", false
	}
	return token, true
}

func authenticateAPIToken(c *gin.Context, plain string) {
	reject := func(reason string) {
		if reason != "" {
			seclog.Warn(c, seclog.TokenRejected, "reason", reason)
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
		c.Abort()
	}
	internalError := func() {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		c.Abort()
	}

	ctx := c.Request.Context()
	token, err := gorm.G[models.APIToken](db.DB).Where("token_hash = ?", apitoken.Hash(plain)).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			reject("unknown_api_token")
			return
		}
		internalError()
		return
	}

	now := time.Now().UTC()
	// Like expired sessions, expired tokens are expected and not a security event.
	if token.IsExpired(now) {
		reject("")
		return
	}
	// A client the user approved for MCP can't use the rest of the API.
	if token.IsOAuth() && c.Request.URL.Path != MCPPath {
		reject("oauth_token_outside_mcp")
		return
	}

	user, err := gorm.G[models.User](db.DB).Where("id = ?", token.UserId).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			reject("unknown_user")
			return
		}
		internalError()
		return
	}

	perms, err := rbac.Load(ctx, db.DB, user.Id)
	if err != nil {
		internalError()
		return
	}

	auth.SetUser(c, &user)
	auth.SetAPIToken(c, &token)
	rbac.Set(c, perms.Restrict(token.Scopes))
	markAPITokenUsed(c, &token, now)
	c.Next()
}

// markAPITokenUsed updates last_used_at and records the first use of a token in
// the audit log. A failed update doesn't fail the request.
func markAPITokenUsed(c *gin.Context, token *models.APIToken, now time.Time) {
	if token.LastUsedAt != nil && now.Sub(*token.LastUsedAt) < apiTokenUsedInterval {
		return
	}

	firstUse := token.LastUsedAt == nil
	query := gorm.G[models.APIToken](db.DB).Where("id = ?", token.Id)
	if firstUse {
		// Only one of several concurrent first requests records the first use.
		query = query.Where("last_used_at IS NULL")
	}
	updated, err := query.Update(c.Request.Context(), "last_used_at", now)
	if err != nil {
		utils.Log.Error().Err(err).Str("api_token_id", token.Id).Msg("Failed to update last use of API token")
		return
	}
	token.LastUsedAt = &now
	// Another request was first
	if updated == 0 {
		return
	}

	sse.PublishUpdate(sse.TopicAPITokensOf(token.UserId), sse.TopicAdminAPITokens)
	if firstUse {
		utils.RecordAuditLog(c, "first-used-api-token", "api-token", token.Id)
	}
}

// RequireSession rejects requests authenticated with an API token. Tokens are
// for automation, so they can't change the account or manage tokens.
func RequireSession() gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, ok := auth.GetAPIToken(c); ok {
			seclog.Warn(c, seclog.AccessDenied, "reason", "session_required")
			c.JSON(http.StatusForbidden, gin.H{"error": "this route can't be used with an api token"})
			c.Abort()
			return
		}
		c.Next()
	}
}
