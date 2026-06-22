package websocket

import (
	"context"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/notifications"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

func handleDeployResult(result *messages.DeployResult, log *zerolog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.Now()

	if result.Success {
		err := updateApplicationStatus(ctx, result.ApplicationId, models.Application{
			SyncStatus:   models.Synced,
			HealthStatus: models.Healthy,
			LastSyncedAt: &now,
		}, log)
		if err != nil {
			return
		}
		notifications.SendNotification(result.ApplicationId, "Success: deployment succeeded for "+result.ApplicationId, log)
		return
	}

	notifications.SendNotification(result.ApplicationId, "Error: deployment failed for "+result.ApplicationId, log)
	_ = updateApplicationStatus(ctx, result.ApplicationId, models.Application{
		SyncStatus:   models.OutOfSync,
		HealthStatus: models.Unhealthy,
	}, log)
}

func updateApplicationStatus(ctx context.Context, applicationID string, updates models.Application, log *zerolog.Logger) error {
	_, err := gorm.G[models.Application](db.DB).
		Where("id = ?", applicationID).
		Updates(ctx, updates)
	if err != nil {
		log.Error().Err(err).Str("applicationId", applicationID).Msg("failed to update application status")
	}
	sse.PublishUpdate("/api/v1/applications")
	return err
}
