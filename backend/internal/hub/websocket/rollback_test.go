package websocket

import (
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/applicationevents"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

func seedRollbackTarget(t *testing.T, appID string) models.ApplicationDeployment {
	t.Helper()
	now := time.Now()
	target := models.ApplicationDeployment{
		ApplicationId: appID,
		Status:        models.DeploymentSucceeded,
		Commit:        "0123456789abcdef",
		CommitMessage: "known good",
		ComposeFile:   crypto.EncryptedString("services: {}\n"),
		Images:        []models.DeployedImage{},
		DeployedAt:    &now,
	}
	if err := gorm.G[models.ApplicationDeployment](db.DB).Create(t.Context(), &target); err != nil {
		t.Fatalf("seed deployment: %v", err)
	}
	return target
}

func startRollbackEvent(t *testing.T, appID string, target *models.ApplicationDeployment, requestID string) {
	t.Helper()
	if _, err := applicationevents.Start(t.Context(), applicationevents.Params{
		ApplicationID: appID,
		RequestID:     &requestID,
		Type:          models.ApplicationEventRollback,
		Source:        models.ApplicationEventSourceManual,
		CommitHash:    &target.Commit,
		DeploymentID:  &target.Id,
	}); err != nil {
		t.Fatalf("start rollback event: %v", err)
	}
}

func loadDeployTestApp(t *testing.T, id string) models.Application {
	t.Helper()
	app, err := gorm.G[models.Application](db.DB).Where("id = ?", id).First(t.Context())
	if err != nil {
		t.Fatalf("load application: %v", err)
	}
	return app
}

func loadEventByRequest(t *testing.T, requestID string) models.ApplicationEvent {
	t.Helper()
	event, err := gorm.G[models.ApplicationEvent](db.DB).Where("request_id = ?", requestID).First(t.Context())
	if err != nil {
		t.Fatalf("load event: %v", err)
	}
	return event
}

func TestHandleDeployResult_RollbackSucceeded(t *testing.T) {
	setupDeployTestEnv(t)
	app := seedDeployApp(t, models.Syncing)
	target := seedRollbackTarget(t, app.Id)
	startRollbackEvent(t, app.Id, &target, "rollback-1")
	nop := zerolog.Nop()
	expectAsyncNotification(t)

	handleDeployResult(t.Context(), "agent-1", &messages.DeployResult{RequestId: "rollback-1", ApplicationId: app.Id, Success: true}, &nop)

	updated := loadDeployTestApp(t, app.Id)
	if updated.SyncStatus != models.OutOfSync {
		t.Errorf("sync status = %q, want out of sync", updated.SyncStatus)
	}
	if updated.RolledBackAt == nil {
		t.Error("expected the application to be marked as rolled back")
	}
	if updated.RolledBackCommit == nil || *updated.RolledBackCommit != target.Commit {
		t.Errorf("rolled back commit = %v, want %q", updated.RolledBackCommit, target.Commit)
	}
	if !updated.AutoSyncPaused {
		t.Error("expected auto-sync to be paused")
	}
	if updated.CurrentDeploymentId == nil || *updated.CurrentDeploymentId != target.Id {
		t.Errorf("current deployment = %v, want %q", updated.CurrentDeploymentId, target.Id)
	}
	if event := loadEventByRequest(t, "rollback-1"); event.Status != models.ApplicationEventSucceeded {
		t.Errorf("event status = %q, want succeeded", event.Status)
	}
	count, err := gorm.G[models.ApplicationDeployment](db.DB).Where("application_id = ?", app.Id).Count(t.Context(), "*")
	if err != nil {
		t.Fatalf("count deployments: %v", err)
	}
	if count != 1 {
		t.Errorf("a rollback must not record a new deployment, got %d", count)
	}
}

func TestHandleDeployResult_RollbackFailed(t *testing.T) {
	setupDeployTestEnv(t)
	app := seedDeployApp(t, models.Syncing)
	target := seedRollbackTarget(t, app.Id)
	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).
		Update(t.Context(), "current_deployment_id", target.Id); err != nil {
		t.Fatalf("set current deployment: %v", err)
	}
	startRollbackEvent(t, app.Id, &target, "rollback-1")
	nop := zerolog.Nop()
	expectAsyncNotification(t)

	handleDeployResult(t.Context(), "agent-1", &messages.DeployResult{RequestId: "rollback-1", ApplicationId: app.Id, ErrorMessage: "pull access denied"}, &nop)

	updated := loadDeployTestApp(t, app.Id)
	if updated.SyncStatus != models.OutOfSync {
		t.Errorf("sync status = %q, want out of sync", updated.SyncStatus)
	}
	if updated.RolledBackAt != nil || updated.AutoSyncPaused {
		t.Error("a failed rollback must not mark the application as rolled back")
	}
	if updated.LastSyncError == nil || *updated.LastSyncError != "rollback failed: pull access denied" {
		t.Errorf("last sync error = %v", updated.LastSyncError)
	}
	if updated.CurrentDeploymentId != nil {
		t.Errorf("expected no current deployment after a failed rollback, got %q", *updated.CurrentDeploymentId)
	}
	event := loadEventByRequest(t, "rollback-1")
	if event.Status != models.ApplicationEventFailed || event.ErrorMessage == nil || *event.ErrorMessage != "pull access denied" {
		t.Errorf("unexpected event: %+v", event)
	}
}

func TestHandleDeployResult_DeploymentClearsRollbackState(t *testing.T) {
	setupDeployTestEnv(t)
	app := seedDeployApp(t, models.Syncing)
	now := time.Now()
	commit := "0123456"
	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).
		Select("RolledBackAt", "RolledBackCommit", "AutoSyncPaused").
		Updates(t.Context(), models.Application{RolledBackAt: &now, RolledBackCommit: &commit, AutoSyncPaused: true}); err != nil {
		t.Fatalf("mark rolled back: %v", err)
	}
	nop := zerolog.Nop()
	expectAsyncNotification(t)

	handleDeployResult(t.Context(), "agent-1", &messages.DeployResult{ApplicationId: app.Id, Success: true}, &nop)

	updated := loadDeployTestApp(t, app.Id)
	if updated.RolledBackAt != nil || updated.RolledBackCommit != nil || updated.AutoSyncPaused {
		t.Errorf("expected the rollback state to be cleared, got %+v", updated)
	}
	if updated.SyncStatus != models.Synced {
		t.Errorf("sync status = %q, want synced", updated.SyncStatus)
	}
}

func TestHandleDeployResult_FailedDeploymentKeepsRollbackState(t *testing.T) {
	setupDeployTestEnv(t)
	app := seedDeployApp(t, models.Syncing)
	now := time.Now()
	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).
		Select("RolledBackAt", "AutoSyncPaused").
		Updates(t.Context(), models.Application{RolledBackAt: &now, AutoSyncPaused: true}); err != nil {
		t.Fatalf("mark rolled back: %v", err)
	}
	nop := zerolog.Nop()
	expectAsyncNotification(t)

	handleDeployResult(t.Context(), "agent-1", &messages.DeployResult{ApplicationId: app.Id, ErrorMessage: "boom"}, &nop)

	if updated := loadDeployTestApp(t, app.Id); updated.RolledBackAt == nil || !updated.AutoSyncPaused {
		t.Error("expected a failed deployment to keep the rollback state")
	}
}

func TestSendAgentSettings_DisablesImagePollingWhileRolledBack(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)
	conn := newTestWSConn(t)
	defer conn.Close() //nolint:errcheck
	client, err := h.Register("agent-1", conn)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	now := time.Now()

	h.SendAgentSettings("agent-1", []models.Application{
		{Id: "polling", ImagePollEnabled: true},
		{Id: "rolled-back", ImagePollEnabled: true, RolledBackAt: &now},
	})

	settings := (<-client.Send).GetAgentSettings().GetImagePollSettings()
	if len(settings) != 2 || !settings[0].Enabled || settings[1].Enabled {
		t.Errorf("expected only the rolled back application to stop polling, got %v", settings)
	}
}
