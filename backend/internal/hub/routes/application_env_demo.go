package routes

import (
	"context"
	"errors"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/danielgtaylor/huma/v2"
	"gorm.io/gorm"
)

const (
	demoEnvWebAppID        = "demo-app-web"
	demoEnvMonitoringAppID = "demo-app-monitoring"
)

func demoEnvFiles(applicationID string) []envFileResponse {
	variable := func(key, value string) envEntryResponse {
		return envEntryResponse{Key: key, Value: &value, IsSet: true}
	}
	secret := func(key string) envEntryResponse {
		return envEntryResponse{Key: key, IsSecret: true, IsSet: true}
	}

	switch applicationID {
	case demoEnvWebAppID:
		return []envFileResponse{{
			Path:     ".env",
			Services: []string{"web"},
			Revision: "demo-revision-web",
			Entries: []envEntryResponse{
				variable("APP_ENV", "production"),
				variable("LOG_LEVEL", "info"),
				secret("DATABASE_PASSWORD"),
				secret("SESSION_SECRET"),
			},
		}}
	case demoEnvMonitoringAppID:
		return []envFileResponse{
			{
				Path:     "prometheus.env",
				Services: []string{"prometheus"},
				Revision: "demo-revision-prometheus",
				Entries: []envEntryResponse{
					variable("PROMETHEUS_RETENTION", "15d"),
					secret("PROMETHEUS_REMOTE_WRITE_TOKEN"),
				},
			},
			{
				Path:     "grafana.env",
				Services: []string{"grafana"},
				Revision: "demo-revision-grafana",
				Entries: []envEntryResponse{
					variable("GF_SERVER_ROOT_URL", "https://grafana.example.com"),
					secret("GF_SECURITY_ADMIN_PASSWORD"),
				},
			},
		}
	default:
		return []envFileResponse{}
	}
}

func listDemoApplicationEnv(ctx context.Context, id string) (*envListOutput, error) {
	application, err := gorm.G[models.Application](db.DB).Where("id = ?", id).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("application not found")
		}
		return nil, internalError()
	}

	return &envListOutput{Body: envListResponse{Files: demoEnvFiles(application.Id)}}, nil
}
