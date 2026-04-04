package auth

import "github.com/gin-gonic/gin"

const claimsContextKey = "auth_claims"

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
