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
	Commit           string
	BuildDate        string
}

var adminSystemInfoConfig AdminSystemInfoConfig

type adminSystemInfoResponse struct {
	Debug            bool     `json:"debug"`
	Host             string   `json:"host"`
	Port             string   `json:"port"`
	LogLevel         string   `json:"logLevel"`
	TrustedProxies   []string `json:"trustedProxies"`
	AppURL           string   `json:"appUrl"`
	DisableLocalAuth bool     `json:"disableLocalAuth"`
	Version          string   `json:"version"`
	Commit           string   `json:"commit"`
	BuildDate        string   `json:"buildDate"`
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
		Commit:           cfg.Commit,
		BuildDate:        cfg.BuildDate,
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
		Commit:           cfg.Commit,
		BuildDate:        cfg.BuildDate,
	})
}
