package websocket

import (
	"context"
	"encoding/json"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/applicationevents"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/deployments"
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

	event, err := applicationevents.ForRequest(ctx, result.RequestId, result.ApplicationId)
	// Without the event a rollback can't be told apart from a deployment. The
	// result is still applied as a deployment's, so nothing stays running, but
	// the rolled back state is kept.
	lookupFailed := err != nil
	if lookupFailed {
		log.Error().Err(err).Str("applicationId", result.ApplicationId).Msg("failed to look up deployment event")
	}
	if event != nil && event.Type == models.ApplicationEventRollback {
		handleRollbackResult(ctx, agentID, app, event, result, log)
		return
	}

	if err := storeComposeWarnings(ctx, agentID, app, result, log); err != nil {
		log.Error().Err(err).Str("applicationId", result.ApplicationId).Msg("failed to store compose warnings")
	}

	if result.Success {
		deployment, err := deployments.Succeed(ctx, result.ApplicationId, result.RequestId, result.Images, app.HealthGate())
		if err != nil {
			log.Error().Err(err).Str("applicationId", result.ApplicationId).Msg("failed to record deployment")
			forgetCurrentDeployment(ctx, result.ApplicationId, log)
		}
		// A deployment under a health gate completes once the gate decided.
		verifying := deployment != nil && deployment.Status == models.DeploymentVerifying
		if !verifying {
			// History reflects the Agent's deployment result independently of whether
			// persisting the application's summary status succeeds.
			completeDeployEvent(ctx, result, models.ApplicationEventSucceeded, nil, log)
		}

		// A successful deploy means the compose project was applied, not that the
		// application is healthy: the agent observes runtime health after the
		// containers start and reports it separately (ApplicationStatusReport), so
		// health stays "unknown" here until that report arrives.
		// Non-nil pointer to "" clears any previous error (GORM skips only nil pointers).
		cleared := ""
		// An empty source clears the stored one when the result has no event.
		var source models.ApplicationEventSource
		if event != nil {
			source = event.Source
		}
		err = updateApplicationStatus(ctx, agentID, result.ApplicationId, models.Application{
			SyncStatus:     models.Synced,
			LastSyncedAt:   &now,
			LastSyncError:  &cleared,
			LastSyncSource: &source,
		}, log)
		if err != nil {
			return
		}
		if !lookupFailed {
			clearRollbackState(ctx, agentID, app, log)
		}
		sendRetentionSettings(ctx, app, log)
		if !verifying {
			go notifications.SendForApplication(result.ApplicationId, models.NotificationEventDeploymentSucceeded, "Success: deployment succeeded for "+app.Name.String(), log)
		}
		return
	}

	errMsg := result.ErrorMessage
	if errMsg == "" {
		errMsg = "deployment failed"
	}
	discarded, err := deployments.Discard(ctx, result.ApplicationId, result.RequestId)
	if err != nil {
		log.Error().Err(err).Str("applicationId", result.ApplicationId).Msg("failed to discard failed deployment")
	}

	_ = updateApplicationStatus(ctx, agentID, result.ApplicationId, models.Application{
		SyncStatus:    models.OutOfSync,
		LastSyncError: &errMsg,
	}, log)
	completeDeployEvent(ctx, result, models.ApplicationEventFailed, &errMsg, log)
	if ctx.Err() != nil {
		return
	}
	go notifications.SendForApplication(result.ApplicationId, models.NotificationEventDeploymentFailed, "Error: deployment failed for "+app.Name.String(), log)

	// Only deployments the hub dispatched are rolled back.
	if event == nil {
		return
	}
	var previous *string
	if discarded != nil {
		previous = discarded.PreviousDeploymentId
	}
	if target := automaticRollbackTarget(ctx, app, previous, log); target != nil {
		triggerAutomaticRollback(ctx, app, target, models.ApplicationEventSourceDeploymentFailure, log)
	}
}

// forgetCurrentDeployment clears the current deployment of an application
// that runs a deployment that couldn't be recorded. Otherwise the earlier
// deployment would still count as running.
func forgetCurrentDeployment(ctx context.Context, applicationID string, log *zerolog.Logger) {
	if err := deployments.ClearCurrent(ctx, applicationID); err != nil {
		log.Error().Err(err).Str("applicationId", applicationID).Msg("failed to clear current deployment")

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

	// Re-read the compose file in the same transaction as the write, so a sync
	// or edit that lands while the deployment ran is not paired with warnings
	// computed for the previous compose file.
	return db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := gorm.G[models.Application](tx).
			Select("Id", "ComposeFile").
			Where("id = ? AND agent_id = ?", app.Id, agentID).
			First(ctx)
		if err != nil {
			return err
		}
		if composewarnings.Hash(current.ComposeFile.String()) != result.ComposeFileSha256 {
			log.Debug().Str("applicationId", app.Id).Msg("compose warnings are stale; application compose file changed")
			return nil
		}

		_, err = gorm.G[models.Application](tx).
			Where("id = ?", app.Id).
			Select("Warnings").
			Updates(ctx, models.Application{Warnings: string(encoded)})
		return err
	})
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

	// Re-read the compose file in the same transaction as the write, so a sync
	// or edit that lands while the deployment ran is not paired with warnings
	// computed for the previous compose file.
	return db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := gorm.G[models.Application](tx).
			Select("Id", "ComposeFile").
			Where("id = ? AND agent_id = ?", app.Id, agentID).
			First(ctx)
		if err != nil {
			return err
		}
		if composewarnings.Hash(current.ComposeFile.String()) != result.ComposeFileSha256 {
			log.Debug().Str("applicationId", app.Id).Msg("compose warnings are stale; application compose file changed")
			return nil
		}

		_, err = gorm.G[models.Application](tx).
			Where("id = ?", app.Id).
			Select("Warnings").
			Updates(ctx, models.Application{Warnings: string(encoded)})
		return err
	})
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
