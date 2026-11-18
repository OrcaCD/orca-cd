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

const CeremonyCookieName = "orcacd_passkey"

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
	setCookie(c, token, defaultCookieConfig)
}

func ClearAuthCookie(c *gin.Context) {
	cfg := defaultCookieConfig
	cfg.MaxAge = -1
	setCookie(c, "", cfg)
}

func GetAuthCookie(c *gin.Context) (string, error) {
	return c.Cookie(defaultCookieConfig.Name)
}

func SetPasskeyCookie(c *gin.Context, id string, lifetime time.Duration) {
	cfg := defaultCookieConfig
	cfg.Name = CeremonyCookieName
	cfg.Path = "/api/v1/auth"
	cfg.MaxAge = int(lifetime / time.Second)
	setCookie(c, id, cfg)
}

func ClearPasskeyCookie(c *gin.Context) {
	SetPasskeyCookie(c, "", -time.Second)
}

func GetPasskeyCookie(c *gin.Context) (string, error) {
	return c.Cookie(CeremonyCookieName)
}

func setCookie(c *gin.Context, value string, cfg CookieConfig) {
	c.SetSameSite(cfg.SameSite)
	c.SetCookie(cfg.Name, value, cfg.MaxAge, cfg.Path, cfg.Domain, cfg.Secure, cfg.HttpOnly)
}
