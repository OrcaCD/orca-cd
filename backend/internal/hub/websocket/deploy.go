package websocket

import (
	"context"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/applicationevents"
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

	app, err := getApplicationByID(ctx, result.ApplicationId, log)
	if err != nil {
		log.Error().Err(err).Str("applicationId", result.ApplicationId).Msg("failed to retrieve application")
		return
	}

	if result.Success {
		// History reflects the Agent's deployment result independently of whether
		// persisting the application's summary status succeeds.
		completeDeployEvent(ctx, result, models.ApplicationEventSucceeded, nil, log)

		// Non-nil pointer to "" clears any previous error (GORM skips only nil pointers).
		cleared := ""
		err := updateApplicationStatus(ctx, result.ApplicationId, models.Application{
			SyncStatus:    models.Synced,
			HealthStatus:  models.Healthy,
			LastSyncedAt:  &now,
			LastSyncError: &cleared,
		}, log)
		if err != nil {
			return
		}
		notifications.SendNotification(result.ApplicationId, "Success: deployment succeeded for "+app.Name.String(), log)
		return
	}

	errMsg := result.ErrorMessage
	if errMsg == "" {
		errMsg = "deployment failed"
	}

	notifications.SendNotification(result.ApplicationId, "Error: deployment failed for "+app.Name.String(), log)
	_ = updateApplicationStatus(ctx, result.ApplicationId, models.Application{
		SyncStatus:    models.OutOfSync,
		HealthStatus:  models.Unhealthy,
		LastSyncError: &errMsg,
	}, log)
	completeDeployEvent(ctx, result, models.ApplicationEventFailed, &errMsg, log)
}

func completeDeployEvent(ctx context.Context, result *messages.DeployResult, status models.ApplicationEventStatus, errorMessage *string, log *zerolog.Logger) {
	matched, err := applicationevents.Complete(ctx, result.RequestId, result.ApplicationId, status, errorMessage)
	if err != nil {
		log.Error().Err(err).Str("applicationId", result.ApplicationId).Msg("failed to complete deployment event")
	} else if !matched && result.RequestId != "" {
		log.Warn().Str("applicationId", result.ApplicationId).Str("requestId", result.RequestId).Msg("deployment result did not match a running event")
	}
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

func getApplicationByID(ctx context.Context, applicationID string, log *zerolog.Logger) (*models.Application, error) {
	app, err := gorm.G[models.Application](db.DB).
		Where("id = ?", applicationID).
		First(ctx)
	if err != nil {
		log.Error().Err(err).Str("applicationId", applicationID).Msg("failed to retrieve application")
		return nil, err
	}
	return &app, nil
}
