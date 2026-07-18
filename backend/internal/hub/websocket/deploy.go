package websocket

import (
	"context"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/applicationevents"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

type notificationRequest struct {
	applicationID string
	message       string
}

func handleDeployResult(result *messages.DeployResult, log *zerolog.Logger) *notificationRequest {
	return handleDeployResultContext(context.Background(), result, log)
}

func handleDeployResultContext(parent context.Context, result *messages.DeployResult, log *zerolog.Logger) *notificationRequest {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	now := time.Now()

	app, err := getApplicationByID(ctx, result.ApplicationId, log)
	if err != nil {
		log.Error().Err(err).Str("applicationId", result.ApplicationId).Msg("failed to retrieve application")
		return nil
	}

	if result.Success {
		// History reflects the Agent's deployment result independently of whether
		// persisting the application's summary status succeeds.
		completeDeployEvent(ctx, result, models.ApplicationEventSucceeded, nil, log)

		// A successful deploy means the compose project was applied, not that the
		// application is healthy: the agent observes runtime health after the
		// containers start and reports it separately (ApplicationStatusReport), so
		// health stays "unknown" here until that report arrives.
		// Non-nil pointer to "" clears any previous error (GORM skips only nil pointers).
		cleared := ""
		err := updateApplicationStatus(ctx, result.ApplicationId, models.Application{
			SyncStatus:    models.Synced,
			LastSyncedAt:  &now,
			LastSyncError: &cleared,
		}, log)
		if err != nil {
			return nil
		}
		return &notificationRequest{
			applicationID: result.ApplicationId,
			message:       "Success: deployment succeeded for " + app.Name.String(),
		}
	}

	errMsg := result.ErrorMessage
	if errMsg == "" {
		errMsg = "deployment failed"
	}

	_ = updateApplicationStatus(ctx, result.ApplicationId, models.Application{
		SyncStatus:    models.OutOfSync,
		HealthStatus:  models.Unhealthy,
		LastSyncError: &errMsg,
	}, log)
	completeDeployEvent(ctx, result, models.ApplicationEventFailed, &errMsg, log)
	return &notificationRequest{
		applicationID: result.ApplicationId,
		message:       "Error: deployment failed for " + app.Name.String(),
	}
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
