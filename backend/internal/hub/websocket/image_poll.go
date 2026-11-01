package websocket

import (
	"context"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/applicationevents"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/deployments"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/notifications"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

func handlePullImagesResult(parent context.Context, client *Client, r *messages.PullImagesResult, log *zerolog.Logger) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()

	app, err := gorm.G[models.Application](db.DB).
		Where("id = ? AND agent_id = ?", r.ApplicationId, client.Id).
		First(ctx)
	if err != nil {
		log.Error().Err(err).
			Str("client", client.Id).
			Str("application_id", r.ApplicationId).
			Msg("failed to retrieve application after image poll")
		return
	}

	if isRepeatedPeriodicFailure(ctx, r, app.SyncStatus) {
		log.Debug().
			Str("client", client.Id).
			Str("application_id", r.ApplicationId).
			Msg("ignoring repeated periodic image poll failure")
		return
	}

	updates := models.Application{}
	if r.Success {
		now := time.Now()
		source := eventSource(ctx, r.RequestId, r.ApplicationId, models.ApplicationEventSourceImagePolling, log)
		updates.LastSyncedAt = &now
		updates.LastSyncSource = &source
		updates.SyncStatus = models.Synced
	} else {
		log.Error().
			Str("client", client.Id).
			Str("application_id", r.ApplicationId).
			Str("error", r.ErrorMessage).
			Msg("image poll failed")
		updates.SyncStatus = models.OutOfSync
	}

	// A result that was already underway when the application was rolled back
	// must not mark it as synced with Git again.
	rolledBack := app.RolledBackAt != nil
	if rolledBack {
		updates.SyncStatus = ""
	}
	if _, err := gorm.G[models.Application](db.DB).
		Where("id = ? AND agent_id = ?", r.ApplicationId, client.Id).
		Updates(ctx, updates); err != nil {
		log.Error().Err(err).
			Str("client", client.Id).
			Str("application_id", r.ApplicationId).
			Msg("failed to update application after image poll")
	}
	if ctx.Err() != nil {
		return
	}

	// An update under a health gate completes once the gate decided.
	var gate time.Duration
	if r.Success && r.ImagesUpdated && !rolledBack {
		gate = app.HealthGate()
	}
	requestID := recordImagePullHistory(ctx, r, gate > 0, log)
	if ctx.Err() != nil {
		return
	}
	if r.Success && r.ImagesUpdated && !rolledBack {
		if _, err := deployments.RecordImageUpdate(ctx, r.ApplicationId, requestID, r.Images, gate); err != nil {
			log.Error().Err(err).Str("application_id", r.ApplicationId).Msg("failed to record image update deployment")
		}
		sendRetentionSettings(ctx, &app, log)
	}

	sse.PublishUpdate(sse.TopicApplications)

	if r.Success {
		if gate == 0 {
			go notifications.SendForApplication(r.ApplicationId, models.NotificationEventImageUpdateSucceeded, "Success: image update succeeded for "+app.Name.String(), log)
		}
		return
	}
	go notifications.SendForApplication(r.ApplicationId, models.NotificationEventImageUpdateFailed, "Error: image update failed for "+app.Name.String(), log)

	// Only a failed redeploy changed the application. A failed check or pull
	// left the containers as they were.
	if r.RedeployFailed {
		if target := automaticRollbackTarget(ctx, &app, nil, log); target != nil {
			triggerAutomaticRollback(ctx, &app, target, models.ApplicationEventSourceDeploymentFailure, log)
		}
	}
}

// startGatedImageUpdateEvent keeps the image update event of a result under a
// health gate running, and records a running event for periodic updates.
func startGatedImageUpdateEvent(ctx context.Context, r *messages.PullImagesResult, log *zerolog.Logger) string {
	event, err := applicationevents.ForRequest(ctx, r.RequestId, r.ApplicationId)
	if err != nil {
		log.Error().Err(err).Str("application_id", r.ApplicationId).Msg("failed to look up image update event")
		return ""
	}
	if event != nil {
		return r.RequestId
	}
	requestID := r.RequestId
	if requestID == "" {
		requestID = uuid.NewString()
	}
	if _, err := applicationevents.Start(ctx, applicationevents.Params{
		ApplicationID: r.ApplicationId,
		RequestID:     &requestID,
		Type:          models.ApplicationEventImageUpdate,
		Source:        models.ApplicationEventSourceImagePolling,
	}); err != nil {
		log.Error().Err(err).Str("application_id", r.ApplicationId).Msg("failed to record periodic image update event")
		return ""
	}
	return requestID
}

// Prevent reporting a lot of failures for periodic image polling with same error message
func isRepeatedPeriodicFailure(ctx context.Context, r *messages.PullImagesResult, syncStatus models.SyncStatus) bool {
	if r.Success || r.RequestId != "" || r.ImagesUpdated || syncStatus != models.OutOfSync {
		return false
	}
	last, err := gorm.G[models.ApplicationEvent](db.DB).
		Where("application_id = ? AND type = ?", r.ApplicationId, models.ApplicationEventImageUpdate).
		Order("created_at DESC, id DESC").
		First(ctx)
	if err != nil {
		return false
	}
	return last.Status == models.ApplicationEventFailed &&
		last.ErrorMessage != nil && *last.ErrorMessage == r.ErrorMessage
}

// recordImagePullHistory completes the explicit image_update event matching this
// result, or records a completed image_polling event for unsolicited periodic
// results that changed images or failed. Successful no-op polls leave no history.
// An update under a health gate stays running instead. It returns the request
// ID of the event, if one was recorded.
func recordImagePullHistory(ctx context.Context, r *messages.PullImagesResult, gated bool, log *zerolog.Logger) string {
	status := models.ApplicationEventFailed
	if r.Success && r.ImagesUpdated {
		status = models.ApplicationEventSucceeded
	} else if r.Success {
		status = models.ApplicationEventNoChange
	}
	var errMsg *string
	if !r.Success && r.ErrorMessage != "" {
		errMsg = &r.ErrorMessage
	}
	if gated {
		return startGatedImageUpdateEvent(ctx, r, log)
	}

	matched, err := applicationevents.Complete(ctx, r.RequestId, r.ApplicationId, status, errMsg)
	if err != nil {
		log.Error().Err(err).Str("application_id", r.ApplicationId).Msg("failed to complete image update event")
		return ""
	}
	if matched {
		return r.RequestId
	}
	if r.Success && !r.ImagesUpdated {
		return ""
	}

	// Periodic updates carry no request ID. One is generated so the deployment
	// recorded for the update can be linked to the event.
	requestID := r.RequestId
	if requestID == "" && status == models.ApplicationEventSucceeded {
		requestID = uuid.NewString()
	}
	params := applicationevents.Params{
		ApplicationID: r.ApplicationId,
		Type:          models.ApplicationEventImageUpdate,
		Source:        models.ApplicationEventSourceImagePolling,
	}
	if requestID != "" {
		params.RequestID = &requestID
	}
	if _, err := applicationevents.RecordCompleted(ctx, params, status, errMsg); err != nil {
		log.Error().Err(err).Str("application_id", r.ApplicationId).Msg("failed to record periodic image update event")
		return ""
	}
	return requestID
}
