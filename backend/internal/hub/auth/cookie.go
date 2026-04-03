package auth

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type CookieConfig struct {
	Name     string
	Path     string
	Domain   string
	MaxAge   int
	Secure   bool
	HttpOnly bool
	SameSite http.SameSite
}

var defaultCookieConfig = CookieConfig{
	Name:     "orcacd_auth",
	Path:     "/",
	Domain:   "",
	MaxAge:   int(tokenExpiry / time.Second),
	Secure:   true,
	HttpOnly: true,
	SameSite: http.SameSiteStrictMode,
}

func SetAuthCookie(c *gin.Context, token string) {
	c.SetSameSite(defaultCookieConfig.SameSite)
	c.SetCookie(defaultCookieConfig.Name, token, defaultCookieConfig.MaxAge, defaultCookieConfig.Path, defaultCookieConfig.Domain, defaultCookieConfig.Secure, defaultCookieConfig.HttpOnly)
}

func ClearAuthCookie(c *gin.Context) {
	c.SetSameSite(defaultCookieConfig.SameSite)
	c.SetCookie(defaultCookieConfig.Name, "", -1, defaultCookieConfig.Path, defaultCookieConfig.Domain, defaultCookieConfig.Secure, defaultCookieConfig.HttpOnly)
}

func GetAuthCookie(c *gin.Context) (string, error) {
	return c.Cookie(defaultCookieConfig.Name)
}
