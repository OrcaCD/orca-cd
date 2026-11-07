package websocket

import (
	"context"
	"encoding/json"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/applicationevents"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/notifications"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/shared/composewarnings"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

func handleDeployResult(parent context.Context, agentID string, result *messages.DeployResult, log *zerolog.Logger) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	now := time.Now()

	app, err := getApplicationByID(ctx, agentID, result.ApplicationId, log)
	if err != nil {
		log.Error().Err(err).Str("applicationId", result.ApplicationId).Msg("failed to retrieve application")
		return
	}

	if err := storeComposeWarnings(ctx, agentID, app, result, log); err != nil {
		log.Error().Err(err).Str("applicationId", result.ApplicationId).Msg("failed to store compose warnings")
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
		source := eventSource(ctx, result.RequestId, result.ApplicationId, "", log)
		err := updateApplicationStatus(ctx, agentID, result.ApplicationId, models.Application{
			SyncStatus:     models.Synced,
			LastSyncedAt:   &now,
			LastSyncError:  &cleared,
			LastSyncSource: &source,
		}, log)
		if err != nil {
			return
		}
		go notifications.SendForApplication(result.ApplicationId, models.NotificationEventDeploymentSucceeded, "Success: deployment succeeded for "+app.Name.String(), log)
		return
	}

	errMsg := result.ErrorMessage
	if errMsg == "" {
		errMsg = "deployment failed"
	}

	_ = updateApplicationStatus(ctx, agentID, result.ApplicationId, models.Application{
		SyncStatus:    models.OutOfSync,
		LastSyncError: &errMsg,
	}, log)
	completeDeployEvent(ctx, result, models.ApplicationEventFailed, &errMsg, log)
	if ctx.Err() == nil {
		go notifications.SendForApplication(result.ApplicationId, models.NotificationEventDeploymentFailed, "Error: deployment failed for "+app.Name.String(), log)
	}
}

func storeComposeWarnings(ctx context.Context, agentID string, app *models.Application, result *messages.DeployResult, log *zerolog.Logger) error {
	if result.ComposeFileSha256 == "" || result.ComposeFileSha256 != composewarnings.Hash(app.ComposeFile.String()) {
		return nil
	}

	warnings := make([]composewarnings.Warning, 0, len(result.Warnings))
	for _, warning := range result.Warnings {
		if warning == nil {
			continue
		}
		warnings = append(warnings, composewarnings.Warning{
			Code:    warning.Code,
			Service: warning.Service,
			Field:   warning.Field,
		})
	}
	encoded, err := json.Marshal(warnings)
	if err != nil {
		return err
	}

	updated, err := gorm.G[models.Application](db.DB).
		Where("id = ? AND agent_id = ? AND compose_file_hash = ?", app.Id, agentID, app.ComposeFileHash).
		Updates(ctx, models.Application{
			ComposeFileHash: crypto.BlindIndex(app.ComposeFile.String()),
			Warnings:        string(encoded),
		})
	if err != nil {
		return err
	}
	if updated == 0 {
		log.Debug().Str("applicationId", app.Id).Msg("compose warnings are stale; application compose file changed")
	}
	return nil
}

func completeDeployEvent(ctx context.Context, result *messages.DeployResult, status models.ApplicationEventStatus, errorMessage *string, log *zerolog.Logger) {
	matched, err := applicationevents.Complete(ctx, result.RequestId, result.ApplicationId, status, errorMessage)
	if err != nil {
		log.Error().Err(err).Str("applicationId", result.ApplicationId).Msg("failed to complete deployment event")
	} else if !matched && result.RequestId != "" {
		log.Warn().Str("applicationId", result.ApplicationId).Str("requestId", result.RequestId).Msg("deployment result did not match a running event")
	}
}

// eventSource resolves what triggered the request so the application can record
// where its last sync came from, falling back when the request has no event.
// An empty fallback clears the stored source (GORM skips only nil pointers).
func eventSource(ctx context.Context, requestID string, applicationID string, fallback models.ApplicationEventSource, log *zerolog.Logger) models.ApplicationEventSource {
	source, found, err := applicationevents.SourceForRequest(ctx, requestID, applicationID)
	if err != nil {
		log.Error().Err(err).Str("applicationId", applicationID).Msg("failed to look up event source")
	}
	if !found {
		return fallback
	}
	return source
}

func updateApplicationStatus(ctx context.Context, agentID string, applicationID string, updates models.Application, log *zerolog.Logger) error {
	_, err := gorm.G[models.Application](db.DB).
		Where("id = ? AND agent_id = ?", applicationID, agentID).
		Updates(ctx, updates)
	if err != nil {
		log.Error().Err(err).Str("applicationId", applicationID).Msg("failed to update application status")
	}
	sse.PublishUpdate(sse.TopicApplications)
	return err
}

func getApplicationByID(ctx context.Context, agentID string, applicationID string, log *zerolog.Logger) (*models.Application, error) {
	app, err := gorm.G[models.Application](db.DB).
		Where("id = ? AND agent_id = ?", applicationID, agentID).
		First(ctx)
	if err != nil {
		log.Error().Err(err).Str("applicationId", applicationID).Msg("failed to retrieve application")
		return nil, err
	}
	return &app, nil
}
