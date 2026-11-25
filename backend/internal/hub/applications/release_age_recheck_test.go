package applications

import (
	"slices"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	hubws "github.com/OrcaCD/orca-cd/internal/hub/websocket"
	"gorm.io/gorm"
)

func seedWaitingImageUpdate(t *testing.T, appID, service, image string) {
	t.Helper()
	eligibleAt := time.Now().Add(time.Hour).UTC()
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

func TestRecheckWaitingImages_PullsAllWaitingImages(t *testing.T) {
	setupTestDB(t)
	if err := db.DB.AutoMigrate(&models.ApplicationImageUpdate{}); err != nil {
		t.Fatalf("migrate image updates: %v", err)
	}
	app, client := seedConnectedApp(t)
	seedWaitingImageUpdate(t, app.Id, "app", "ghcr.io/org/app:1.0")
	seedWaitingImageUpdate(t, app.Id, "db", "postgres:18")

	RecheckWaitingImages(t.Context(), app.Id)
	got := awaitPullRequest(t, client).WebhookImages
	slices.Sort(got)
	if !slices.Equal(got, []string{"ghcr.io/org/app:1.0", "postgres:18"}) {
		t.Errorf("pulled images = %v, want both waiting images", got)
	}
	expectNoPullRequest(t, client, 50*time.Millisecond)
}

func TestRecheckWaitingImages_SkipsApplicationsItCantPull(t *testing.T) {
	tests := map[string]func(t *testing.T, app *models.Application){
		"image polling enabled": func(t *testing.T, app *models.Application) {
			if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).
				Update(t.Context(), "image_poll_enabled", true); err != nil {
				t.Fatalf("enable image polling: %v", err)
			}
		},
		"agent offline": func(_ *testing.T, app *models.Application) {
			hubws.DefaultHub.Unregister(app.AgentId)
		},
	}
	for name, prepare := range tests {
		t.Run(name, func(t *testing.T) {
			setupTestDB(t)
			if err := db.DB.AutoMigrate(&models.ApplicationImageUpdate{}); err != nil {
				t.Fatalf("migrate image updates: %v", err)
			}
			app, client := seedConnectedApp(t)
			seedWaitingImageUpdate(t, app.Id, "app", "ghcr.io/org/app:1.0")
			prepare(t, app)

			RecheckWaitingImages(t.Context(), app.Id)
			expectNoPullRequest(t, client, 50*time.Millisecond)
		})
	}
}

func TestRecheckWaitingImages_IgnoresAFailingQuery(t *testing.T) {
	setupTestDB(t)
	app, client := seedConnectedApp(t)

	// The image updates table is missing, so loading the waiting updates fails.
	RecheckWaitingImages(t.Context(), app.Id)
	expectNoPullRequest(t, client, 50*time.Millisecond)
}
