package auth

import "github.com/gin-gonic/gin"

const claimsContextKey = "auth_claims"

func SetClaims(c *gin.Context, claims *Claims) {
	c.Set(claimsContextKey, claims)
}

func GetClaims(c *gin.Context) (*Claims, bool) {
	val, exists := c.Get(claimsContextKey)
	if !exists {
		return nil, false
	}
	claims, ok := val.(*Claims)
	return claims, ok
}
