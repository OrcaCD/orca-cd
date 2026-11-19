package routes

import (
	"context"
	"net/http"

	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/danielgtaylor/huma/v2"
)

type AdminSystemInfoConfig struct {
	Host             string
	Port             string
	LogLevel         string
	TrustedProxies   []string
	AllowedIPs       []string
	AppURL           string
	DisableLocalAuth bool
	Version          string
	Commit           string
	BuildDate        string
}

var adminSystemInfoConfig AdminSystemInfoConfig

const demoRedacted = "hidden in demo mode"

type adminSystemInfoResponse struct {
	Host             string   `json:"host" required:"true"`
	Port             string   `json:"port" required:"true"`
	LogLevel         string   `json:"logLevel" required:"true"`
	TrustedProxies   []string `json:"trustedProxies" required:"true" doc:"null when no proxy is trusted"`
	AllowedIPs       []string `json:"allowedIps" required:"true" doc:"IP addresses and CIDR ranges the hub accepts requests from. null allows any."`
	AppURL           string   `json:"appUrl" required:"true"`
	DisableLocalAuth bool     `json:"disableLocalAuth" required:"true" doc:"Whether login with a password or passkey is disabled"`
	Version          string   `json:"version" required:"true"`
	Commit           string   `json:"commit" required:"true"`
	BuildDate        string   `json:"buildDate" required:"true"`
}

func SetAdminSystemInfoConfig(cfg AdminSystemInfoConfig) {
	adminSystemInfoConfig = AdminSystemInfoConfig{
		Host:             cfg.Host,
		Port:             cfg.Port,
		LogLevel:         cfg.LogLevel,
		TrustedProxies:   append([]string(nil), cfg.TrustedProxies...),
		AllowedIPs:       append([]string(nil), cfg.AllowedIPs...),
		AppURL:           cfg.AppURL,
		DisableLocalAuth: cfg.DisableLocalAuth,
		Version:          cfg.Version,
		Commit:           cfg.Commit,
		BuildDate:        cfg.BuildDate,
	}
}

var AdminSystemInfoHandler = restapi.Handler(huma.Operation{
	OperationID: "get-system-info",
	Method:      http.MethodGet,
	Path:        "/admin/system-info",
	Summary:     "Get the hub's configuration",
	Description: "Returns the configuration and version of the hub. The network settings are hidden in demo mode. " + requiresAdmin,
	Tags:        []string{tagAdmin},
	Security:    restapi.Authenticated,
}, func(_ context.Context, _ *struct{}) (*struct{ Body adminSystemInfoResponse }, error) {
	cfg := adminSystemInfoConfig

	if DemoMode {
		cfg.Host = demoRedacted
		cfg.Port = demoRedacted
		cfg.TrustedProxies = []string{demoRedacted}
		cfg.AllowedIPs = []string{demoRedacted}
	}

	return &struct{ Body adminSystemInfoResponse }{Body: adminSystemInfoResponse{
		Host:             cfg.Host,
		Port:             cfg.Port,
		LogLevel:         cfg.LogLevel,
		TrustedProxies:   append([]string(nil), cfg.TrustedProxies...),
		AllowedIPs:       append([]string(nil), cfg.AllowedIPs...),
		AppURL:           cfg.AppURL,
		DisableLocalAuth: cfg.DisableLocalAuth,
		Version:          cfg.Version,
		Commit:           cfg.Commit,
		BuildDate:        cfg.BuildDate,
	}}, nil
})
