package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type AdminSystemInfoConfig struct {
	Debug            bool
	Host             string
	Port             string
	LogLevel         string
	TrustedProxies   []string
	AppURL           string
	DisableLocalAuth bool
	Version          string
}

var adminSystemInfoConfig AdminSystemInfoConfig

type adminSystemInfoResponse struct {
	Debug            bool     `json:"debug"`
	Host             string   `json:"host"`
	Port             string   `json:"port"`
	LogLevel         string   `json:"log_level"`
	TrustedProxies   []string `json:"trusted_proxies"`
	AppURL           string   `json:"app_url"`
	DisableLocalAuth bool     `json:"disable_local_auth"`
	Version          string   `json:"version"`
}

func SetAdminSystemInfoConfig(cfg AdminSystemInfoConfig) {
	adminSystemInfoConfig = AdminSystemInfoConfig{
		Debug:            cfg.Debug,
		Host:             cfg.Host,
		Port:             cfg.Port,
		LogLevel:         cfg.LogLevel,
		TrustedProxies:   append([]string(nil), cfg.TrustedProxies...),
		AppURL:           cfg.AppURL,
		DisableLocalAuth: cfg.DisableLocalAuth,
		Version:          cfg.Version,
	}
}

func AdminSystemInfoHandler(c *gin.Context) {
	cfg := adminSystemInfoConfig

	c.JSON(http.StatusOK, adminSystemInfoResponse{
		Debug:            cfg.Debug,
		Host:             cfg.Host,
		Port:             cfg.Port,
		LogLevel:         cfg.LogLevel,
		TrustedProxies:   append([]string(nil), cfg.TrustedProxies...),
		AppURL:           cfg.AppURL,
		DisableLocalAuth: cfg.DisableLocalAuth,
		Version:          cfg.Version,
	})
}
