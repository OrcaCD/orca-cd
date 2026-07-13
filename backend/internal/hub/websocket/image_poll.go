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

func handlePullImagesResult(client *Client, r *messages.PullImagesResult, log *zerolog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

	updates := models.Application{}
	if r.Success {
		now := time.Now()
		updates.LastSyncedAt = &now
		updates.SyncStatus = models.Synced
	} else {
		log.Error().
			Str("client", client.Id).
			Str("application_id", r.ApplicationId).
			Str("error", r.ErrorMessage).
			Msg("image poll failed")
		updates.SyncStatus = models.OutOfSync
	}

	if _, err := gorm.G[models.Application](db.DB).
		Where("id = ? AND agent_id = ?", r.ApplicationId, client.Id).
		Updates(ctx, updates); err != nil {
		log.Error().Err(err).
			Str("client", client.Id).
			Str("application_id", r.ApplicationId).
			Msg("failed to update application after image poll")
	}

	recordImagePullHistory(ctx, r, log)

	if r.Success {
		notifications.SendNotification(r.ApplicationId, "Success: image update succeeded for "+app.Name.String(), log)
	} else {
		notifications.SendNotification(r.ApplicationId, "Error: image update failed for "+app.Name.String(), log)
	}

	sse.PublishUpdate("/api/v1/applications")
}

// recordImagePullHistory completes the explicit image_update event matching this
// result, or records a completed image_polling event for unsolicited periodic
// results that changed images or failed. Successful no-op polls leave no history.
func recordImagePullHistory(ctx context.Context, r *messages.PullImagesResult, log *zerolog.Logger) {
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

	matched, err := applicationevents.Complete(ctx, r.RequestId, r.ApplicationId, status, errMsg)
	if err != nil {
		log.Error().Err(err).Str("application_id", r.ApplicationId).Msg("failed to complete image update event")
		return
	}
	if matched {
		return
	}
	if r.Success && !r.ImagesUpdated {
		return
	}

	params := applicationevents.Params{
		ApplicationID: r.ApplicationId,
		Type:          models.ApplicationEventImageUpdate,
		Source:        models.ApplicationEventSourceImagePolling,
	}
	if r.RequestId != "" {
		requestID := r.RequestId
		params.RequestID = &requestID
	}
	if _, err := applicationevents.RecordCompleted(ctx, params, status, errMsg); err != nil {
		log.Error().Err(err).Str("application_id", r.ApplicationId).Msg("failed to record periodic image update event")
	}
}
