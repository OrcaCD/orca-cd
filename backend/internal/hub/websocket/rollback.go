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

// handleRollbackResult applies the agent's result of a rollback. A rolled back
// application runs the target deployment, which drifts from its commit in Git,
// so it stays out of sync. A failed rollback is not retried.
func handleRollbackResult(ctx context.Context, agentID string, app *models.Application, event *models.ApplicationEvent, result *messages.DeployResult, log *zerolog.Logger) {
	if !result.Success {
		errMsg := result.ErrorMessage
		if errMsg == "" {
			errMsg = "rollback failed"
		}
		completeDeployEvent(ctx, result, models.ApplicationEventFailed, &errMsg, log)
		lastSyncError := "rollback failed: " + errMsg
		// What runs after a failed rollback is unknown.
		if _, err := gorm.G[models.Application](db.DB).
			Where("id = ? AND agent_id = ?", app.Id, agentID).
			Select("SyncStatus", "LastSyncError", "CurrentDeploymentId").
			Updates(ctx, models.Application{SyncStatus: models.OutOfSync, LastSyncError: &lastSyncError}); err != nil {
			log.Error().Err(err).Str("applicationId", app.Id).Msg("failed to update application after failed rollback")
		}
		sse.PublishUpdate(sse.TopicApplications)
		if ctx.Err() == nil {
			go notifications.SendForApplication(app.Id, models.NotificationEventRollbackFailed, "Error: rollback failed for "+app.Name.String()+": "+errMsg, log)
		}
		return
	}

	completeDeployEvent(ctx, result, models.ApplicationEventSucceeded, nil, log)
	now := time.Now()
	if _, err := gorm.G[models.Application](db.DB).
		Where("id = ? AND agent_id = ?", app.Id, agentID).
		Select("SyncStatus", "CurrentDeploymentId", "RolledBackAt", "RolledBackCommit", "AutoSyncPaused").
		Updates(ctx, models.Application{
			SyncStatus:          models.OutOfSync,
			CurrentDeploymentId: event.DeploymentId,
			RolledBackAt:        &now,
			RolledBackCommit:    event.CommitHash,
			AutoSyncPaused:      true,
		}); err != nil {
		log.Error().Err(err).Str("applicationId", app.Id).Msg("failed to mark application as rolled back")
		return
	}
	sse.PublishUpdate(sse.TopicApplications)
	sendAgentSettings(ctx, agentID, log)

	message := "Warning: " + app.Name.String() + " was rolled back"
	if event.CommitHash != nil && *event.CommitHash != "" {
		message += " to " + shortCommit(*event.CommitHash)
	}
	go notifications.SendForApplication(app.Id, models.NotificationEventRollbackSucceeded, message, log)
}

// clearRollbackState ends the rolled back state once the application was
// deployed from Git again.
func clearRollbackState(ctx context.Context, agentID string, app *models.Application, log *zerolog.Logger) {
	if app.RolledBackAt == nil && !app.AutoSyncPaused {
		return
	}
	if _, err := gorm.G[models.Application](db.DB).
		Where("id = ? AND agent_id = ?", app.Id, agentID).
		Select("RolledBackAt", "RolledBackCommit", "AutoSyncPaused").
		Updates(ctx, models.Application{}); err != nil {
		log.Error().Err(err).Str("applicationId", app.Id).Msg("failed to clear rollback state")
		return
	}
	sse.PublishUpdate(sse.TopicApplications)
	sendAgentSettings(ctx, agentID, log)
}

// sendAgentSettings pushes the agent's image poll settings again, which a
// rollback pauses.
func sendAgentSettings(ctx context.Context, agentID string, log *zerolog.Logger) {
	if DefaultHub == nil {
		return
	}
	apps, err := gorm.G[models.Application](db.DB).Where("agent_id = ?", agentID).Find(ctx)
	if err != nil {
		log.Error().Err(err).Str("agent_id", agentID).Msg("failed to load applications for agent settings")
		return
	}
	DefaultHub.SendAgentSettings(agentID, apps)
}

func shortCommit(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}
