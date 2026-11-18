package auth

import (
	"strings"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
)

const (
	claimsContextKey   = "auth_claims"
	userContextKey     = "auth_user"
	apiTokenContextKey = "auth_api_token" //nolint:gosec // context key, not a credential
)

func SetClaims(c *gin.Context, claims *UserClaims) {
	c.Set(claimsContextKey, claims)
}

func GetClaims(c *gin.Context) (*UserClaims, bool) {
	val, exists := c.Get(claimsContextKey)
	if !exists {
		return nil, false
	}
	claims, ok := val.(*UserClaims)
	return claims, ok
}

// SetUser stores the authenticated user as loaded from the database for the current request.
func SetUser(c *gin.Context, user *models.User) {
	c.Set(userContextKey, user)
}

func GetUser(c *gin.Context) (*models.User, bool) {
	val, exists := c.Get(userContextKey)
	if !exists {
		return nil, false
	}
	user, ok := val.(*models.User)
	return user, ok
}

// SetAPIToken stores the API token the current request was authenticated with.
func SetAPIToken(c *gin.Context, token *models.APIToken) {
	c.Set(apiTokenContextKey, token)
}

// GetAPIToken returns the API token of the current request. It reports false
// for requests authenticated with the session cookie.
func GetAPIToken(c *gin.Context) (*models.APIToken, bool) {
	val, exists := c.Get(apiTokenContextKey)
	if !exists {
		return nil, false
	}
	token, ok := val.(*models.APIToken)
	return token, ok
}

// GetBearerToken returns the token of an "Authorization: Bearer" header.
func GetBearerToken(c *gin.Context) (string, bool) {
	scheme, token, ok := strings.Cut(c.GetHeader("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}
