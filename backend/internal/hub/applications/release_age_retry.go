package applications

import (
	"context"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	hubws "github.com/OrcaCD/orca-cd/internal/hub/websocket"
	"gorm.io/gorm"
)

const (
	releaseAgeRetryInterval = time.Minute
	// agentClockTolerance delays a retry, so an agent whose clock is a little
	// behind doesn't hold the update back again.
	agentClockTolerance = time.Minute
)

// waitingImages are the images of an application whose update waits for the
// minimum release age.
type waitingImages struct {
	images []string
	// eligibleAt is when the last of them is old enough.
	eligibleAt time.Time
}

// StartReleaseAgeRetry pulls the images of updates that waited for the minimum
// release age again once they are old enough. Without image polling nothing
// else would.
func StartReleaseAgeRetry() (stop func()) {
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		// retried holds the eligible time each application was last pulled for,
		// so a pull that leaves its updates waiting isn't repeated every tick.
		retried := map[string]time.Time{}
		ticker := time.NewTicker(releaseAgeRetryInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				retryEligibleImagePulls(ctx, retried)
			case <-ctx.Done():
				return
			}
		}
	}()
	return func() {
		cancel()
		<-stopped
	}
}

func retryEligibleImagePulls(ctx context.Context, retried map[string]time.Time) {
	updates, err := gorm.G[models.ApplicationImageUpdate](db.DB).
		Where("status = ? AND eligible_at <= ?", models.ImageUpdateWaiting, time.Now().UTC().Add(-agentClockTolerance)).
		Find(ctx)
	if err != nil {
		Log.Error().Err(err).Msg("failed to load the image updates waiting for the minimum release age")
		return
	}
	for appID, waiting := range groupWaitingImages(updates) {
		if retried[appID].Equal(waiting.eligibleAt) {
			continue
		}
		if pullWaitingImages(ctx, appID, waiting.images) {
			retried[appID] = waiting.eligibleAt
		}
	}
}

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
	for appID, waiting := range groupWaitingImages(updates) {
		pullWaitingImages(ctx, appID, waiting.images)
	}
}

func groupWaitingImages(updates []models.ApplicationImageUpdate) map[string]*waitingImages {
	byApplication := map[string]*waitingImages{}
	for _, update := range updates {
		waiting, ok := byApplication[update.ApplicationId]
		if !ok {
			waiting = &waitingImages{}
			byApplication[update.ApplicationId] = waiting
		}
		waiting.images = appendImage(waiting.images, update.Image)
		if update.EligibleAt != nil && update.EligibleAt.After(waiting.eligibleAt) {
			waiting.eligibleAt = *update.EligibleAt
		}
	}
	return byApplication
}

// pullWaitingImages pulls the images of the application and reports whether the
// pull was sent. Applications that poll their images are left to the polling,
// and an offline agent is retried later.
func pullWaitingImages(ctx context.Context, appID string, images []string) bool {
	app, err := gorm.G[models.Application](db.DB).Where("id = ?", appID).First(ctx)
	if err != nil {
		Log.Error().Err(err).Str("applicationId", appID).Msg("failed to load application for waiting image updates")
		return false
	}
	if app.ImagePollEnabled || app.RolledBackAt != nil || hubws.DefaultHub == nil {
		return false
	}
	if _, connected := hubws.DefaultHub.GetClient(app.AgentId); !connected {
		return false
	}
	// Unlike TriggerImagePull, this leaves a queued webhook pull alone, as this one
	// only pulls the waiting images.
	return triggerImagePull(&app, SyncOrigin{Source: models.ApplicationEventSourceImagePolling, WebhookImages: images})
}
