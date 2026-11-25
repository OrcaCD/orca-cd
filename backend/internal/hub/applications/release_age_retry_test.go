package applications

import (
	"slices"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"gorm.io/gorm"
)

func seedWaitingImageUpdate(t *testing.T, appID, service, image string, eligibleAt time.Time) {
	t.Helper()
	eligibleAt = eligibleAt.UTC()
	update := models.ApplicationImageUpdate{
		ApplicationId: appID,
		Service:       service,
		Image:         image,
		Status:        models.ImageUpdateWaiting,
		EligibleAt:    &eligibleAt,
		CheckedAt:     time.Now().UTC(),
	}
	if err := gorm.G[models.ApplicationImageUpdate](db.DB).Create(t.Context(), &update); err != nil {
		t.Fatalf("seed image update: %v", err)
	}
}

func setupWaitingImageUpdates(t *testing.T) (*models.Application, func() []string) {
	t.Helper()
	setupTestDB(t)
	if err := db.DB.AutoMigrate(&models.ApplicationImageUpdate{}); err != nil {
		t.Fatalf("migrate image updates: %v", err)
	}
	app, client := seedConnectedApp(t)
	seedWaitingImageUpdate(t, app.Id, "app", "ghcr.io/org/app:1.0", time.Now().Add(-time.Hour))
	seedWaitingImageUpdate(t, app.Id, "db", "postgres:18", time.Now().Add(time.Hour))
	pulledImages := func() []string { return awaitPullRequest(t, client).WebhookImages }
	t.Cleanup(func() { expectNoPullRequest(t, client, 50*time.Millisecond) })
	return app, pulledImages
}

func TestRetryEligibleImagePulls_PullsEligibleImagesOnce(t *testing.T) {
	_, pulledImages := setupWaitingImageUpdates(t)
	retried := map[string]time.Time{}

	retryEligibleImagePulls(t.Context(), retried)
	if got := pulledImages(); !slices.Equal(got, []string{"ghcr.io/org/app:1.0"}) {
		t.Errorf("pulled images = %v, want only the eligible one", got)
	}

	// The agent didn't report back, so the update still waits with the same time.
	retryEligibleImagePulls(t.Context(), retried)
}

func TestRetryEligibleImagePulls_LeavesPollingApplicationsToPolling(t *testing.T) {
	app, _ := setupWaitingImageUpdates(t)
	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).
		Update(t.Context(), "image_poll_enabled", true); err != nil {
		t.Fatalf("enable image polling: %v", err)
	}

	retryEligibleImagePulls(t.Context(), map[string]time.Time{})
}

func TestRecheckWaitingImages_PullsAllWaitingImages(t *testing.T) {
	app, pulledImages := setupWaitingImageUpdates(t)

	RecheckWaitingImages(t.Context(), app.Id)
	if got := pulledImages(); !slices.Equal(got, []string{"ghcr.io/org/app:1.0", "postgres:18"}) &&
		!slices.Equal(got, []string{"postgres:18", "ghcr.io/org/app:1.0"}) {
		t.Errorf("pulled images = %v, want both waiting images", got)
	}
}
