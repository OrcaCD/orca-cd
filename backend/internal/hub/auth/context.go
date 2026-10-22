package auth

import (
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
)

const (
	claimsContextKey = "auth_claims"
	userContextKey   = "auth_user"
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
