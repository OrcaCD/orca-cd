package routes

import (
	"errors"
	"net/http"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
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
		return []envFileResponse{{
			Path:     "monitoring/.env",
			Services: []string{"prometheus", "grafana"},
			Revision: "demo-revision-monitoring",
			Entries: []envEntryResponse{
				variable("GF_SERVER_ROOT_URL", "https://grafana.example.com"),
				variable("PROMETHEUS_RETENTION", "15d"),
				secret("GF_SECURITY_ADMIN_PASSWORD"),
			},
		}}
	default:
		return []envFileResponse{}
	}
}

func listDemoApplicationEnv(c *gin.Context) {
	application, err := gorm.G[models.Application](db.DB).Where("id = ?", c.Param("id")).First(c.Request.Context())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "application not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}

	c.JSON(http.StatusOK, envListResponse{Files: demoEnvFiles(application.Id)})
}
