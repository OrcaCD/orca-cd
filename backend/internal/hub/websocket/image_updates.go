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

var imageUpdateStatuses = map[messages.ImageUpdateStatus]models.ImageUpdateStatus{
	messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_UP_TO_DATE: models.ImageUpdateUpToDate,
	messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_WAITING:    models.ImageUpdateWaiting,
	messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_BLOCKED:    models.ImageUpdateBlocked,
	messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_UPDATED:    models.ImageUpdateUpdated,
	messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_FAILED:     models.ImageUpdateFailed,
}

// handleImageUpdatesReport stores the update state of every image an image
// check of the agent saw. Services the check skipped keep their last state,
// services that are no longer reported are removed.
func handleImageUpdatesReport(parent context.Context, client *Client, r *messages.ImageUpdatesReport, log *zerolog.Logger) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()

	// An agent may only report the applications assigned to it.
	if _, err := gorm.G[models.Application](db.DB).
		Where("id = ? AND agent_id = ?", r.ApplicationId, client.Id).
		First(ctx); err != nil {
		log.Warn().Err(err).Str("client", client.Id).Str("application_id", r.ApplicationId).
			Msg("ignoring image updates of an unknown application")
		return
	}

	checkedAt := time.Unix(r.CheckedAt, 0).UTC()
	if r.CheckedAt == 0 {
		checkedAt = time.Now().UTC()
	}
	rows := make([]models.ApplicationImageUpdate, 0, len(r.Images))
	var skipped []string
	for _, image := range r.Images {
		status, ok := imageUpdateStatuses[image.Status]
		if !ok {
			skipped = append(skipped, image.Service)
			continue
		}
		rows = append(rows, models.ApplicationImageUpdate{
			ApplicationId:       r.ApplicationId,
			Service:             image.Service,
			Image:               image.Image,
			Status:              status,
			CurrentDigest:       image.CurrentDigest,
			CurrentVersion:      image.CurrentVersion,
			NewDigest:           image.NewDigest,
			NewVersion:          image.NewVersion,
			PushedAt:            unixTime(image.PushedAt),
			FirstSeen:           image.FirstSeen,
			EligibleAt:          unixTime(image.EligibleAt),
			MinimumAgeSeconds:   image.MinimumAgeSeconds,
			MinimumAgeFromLabel: image.MinimumAgeFromLabel,
			Reason:              image.Reason,
			CheckedAt:           checkedAt,
		})
	}

	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		stale := gorm.G[models.ApplicationImageUpdate](tx).Where("application_id = ?", r.ApplicationId)
		if len(skipped) > 0 {
			stale = stale.Where("service NOT IN ?", skipped)
		}
		if _, err := stale.Delete(ctx); err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		return gorm.G[models.ApplicationImageUpdate](tx).CreateInBatches(ctx, &rows, 100)
	})
	if err != nil {
		log.Error().Err(err).Str("application_id", r.ApplicationId).Msg("failed to store image updates")
		return
	}
	sse.PublishUpdate(sse.TopicApplicationImageUpdates(r.ApplicationId))
}

func unixTime(seconds int64) *time.Time {
	if seconds == 0 {
		return nil
	}
	t := time.Unix(seconds, 0).UTC()
	return &t
}
