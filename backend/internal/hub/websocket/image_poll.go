package websocket

import (
	"context"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

func handlePullImagesResult(client *Client, r *messages.PullImagesResult, log *zerolog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

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
	sse.PublishUpdate("/api/v1/applications")
}
