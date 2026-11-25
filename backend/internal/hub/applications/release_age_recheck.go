package applications

import (
	"context"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	hubws "github.com/OrcaCD/orca-cd/internal/hub/websocket"
	"gorm.io/gorm"
)

// RecheckWaitingImages pulls the images of waiting updates right away after the
// minimum release age changed, so they are checked against the new age. Without
// application IDs, it rechecks those of all applications.
func RecheckWaitingImages(ctx context.Context, applicationIDs ...string) {
	query := gorm.G[models.ApplicationImageUpdate](db.DB).Where("status = ?", models.ImageUpdateWaiting)
	if len(applicationIDs) > 0 {
		query = query.Where("application_id IN ?", applicationIDs)
	}
	updates, err := query.Find(ctx)
	if err != nil {
		Log.Error().Err(err).Msg("failed to load the image updates waiting for the minimum release age")
		return
	}
	imagesByApplication := map[string][]string{}
	for _, update := range updates {
		imagesByApplication[update.ApplicationId] = appendImage(imagesByApplication[update.ApplicationId], update.Image)
	}
	for appID, images := range imagesByApplication {
		pullWaitingImages(ctx, appID, images)
	}
}

// pullWaitingImages pulls the images of the application. Applications that poll
// their images are left to the polling.
func pullWaitingImages(ctx context.Context, appID string, images []string) {
	app, err := gorm.G[models.Application](db.DB).Where("id = ?", appID).First(ctx)
	if err != nil {
		Log.Error().Err(err).Str("applicationId", appID).Msg("failed to load application for waiting image updates")
		return
	}
	if app.ImagePollEnabled || app.RolledBackAt != nil || hubws.DefaultHub == nil {
		return
	}
	if _, connected := hubws.DefaultHub.GetClient(app.AgentId); !connected {
		return
	}
	// Unlike TriggerImagePull, this leaves a queued webhook pull alone, as this one
	// only pulls the waiting images.
	triggerImagePull(&app, SyncOrigin{Source: models.ApplicationEventSourceImagePolling, WebhookImages: images})
}
