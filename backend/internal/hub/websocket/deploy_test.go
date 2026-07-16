package websocket

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/applicationevents"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func setupDeployTestEnv(t *testing.T) {
	t.Helper()

	if err := crypto.Init("test-secret-that-is-long-enough-32chars"); err != nil {
		t.Fatalf("failed to init crypto: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	testDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: gormlogger.New(
			log.New(os.Stderr, "\n", log.LstdFlags),
			gormlogger.Config{LogLevel: gormlogger.Warn, IgnoreRecordNotFoundError: true},
		),
	})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	// Application + Notification create the application_notifications join table that
	// SendNotification queries, so the notification path runs cleanly (no rows).
	if err := testDB.AutoMigrate(&models.Repository{}, &models.Application{}, &models.Notification{}, &models.ApplicationEvent{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	sqlDB, err := testDB.DB()
	if err != nil {
		t.Fatalf("failed to get sql db: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
		db.DB = nil
	})

	db.DB = testDB
}

func seedDeployApp(t *testing.T, syncStatus models.SyncStatus) models.Application {
	t.Helper()
	app := models.Application{
		Name:         crypto.EncryptedString("test-app"),
		AgentId:      "agent-1",
		SyncStatus:   syncStatus,
		HealthStatus: models.UnknownHealth,
		Branch:       "main",
		Path:         "deploy.yml",
		ComposeFile:  crypto.EncryptedString("version: '3.9'\n"),
	}
	if err := db.DB.Select("*").Create(&app).Error; err != nil {
		t.Fatalf("failed to seed application: %v", err)
	}
	return app
}

func TestHandleDeployResult_Success_SetsSynced(t *testing.T) {
	setupDeployTestEnv(t)
	app := seedDeployApp(t, models.Syncing)
	nop := zerolog.Nop()

	handleDeployResult(&messages.DeployResult{ApplicationId: app.Id, Success: true}, &nop)

	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(context.Background())
	if err != nil {
		t.Fatalf("failed to load application: %v", err)
	}
	if updated.SyncStatus != models.Synced {
		t.Errorf("expected SyncStatus %q, got %q", models.Synced, updated.SyncStatus)
	}
	// Health is not assumed on deploy success; it stays unknown until the agent's
	// post-deploy status report arrives.
	if updated.HealthStatus != models.UnknownHealth {
		t.Errorf("expected HealthStatus %q, got %q", models.UnknownHealth, updated.HealthStatus)
	}
	if updated.LastSyncedAt == nil {
		t.Error("expected LastSyncedAt to be set")
	}
}

func TestHandleDeployResultCompletesMatchingEvent(t *testing.T) {
	setupDeployTestEnv(t)
	app := seedDeployApp(t, models.Syncing)
	requestID := "deploy-request"
	if _, err := applicationevents.Start(t.Context(), applicationevents.Params{
		ApplicationID: app.Id,
		RequestID:     &requestID,
		Type:          models.ApplicationEventDeployment,
		Source:        models.ApplicationEventSourceManual,
	}); err != nil {
		t.Fatalf("start event: %v", err)
	}
	nop := zerolog.Nop()
	handleDeployResult(&messages.DeployResult{RequestId: requestID, ApplicationId: app.Id, Success: true}, &nop)
	event, err := gorm.G[models.ApplicationEvent](db.DB).Where("request_id = ?", requestID).First(t.Context())
	if err != nil || event.Status != models.ApplicationEventSucceeded {
		t.Fatalf("event=%+v err=%v", event, err)
	}
}

func TestHandleDeployResultDoesNotCompleteMismatchedEvent(t *testing.T) {
	setupDeployTestEnv(t)
	app := seedDeployApp(t, models.Syncing)
	requestID := "expected-request"
	if _, err := applicationevents.Start(t.Context(), applicationevents.Params{
		ApplicationID: app.Id,
		RequestID:     &requestID,
		Type:          models.ApplicationEventDeployment,
		Source:        models.ApplicationEventSourceManual,
	}); err != nil {
		t.Fatalf("start event: %v", err)
	}
	nop := zerolog.Nop()
	handleDeployResult(&messages.DeployResult{RequestId: "other-request", ApplicationId: app.Id, Success: true}, &nop)
	event, err := gorm.G[models.ApplicationEvent](db.DB).Where("request_id = ?", requestID).First(t.Context())
	if err != nil || event.Status != models.ApplicationEventRunning {
		t.Fatalf("event=%+v err=%v", event, err)
	}
}

func TestHandleDeployResult_Failure_SetsOutOfSync(t *testing.T) {
	setupDeployTestEnv(t)
	app := seedDeployApp(t, models.Syncing)
	nop := zerolog.Nop()

	handleDeployResult(&messages.DeployResult{
		ApplicationId: app.Id,
		Success:       false,
		ErrorMessage:  "boom",
	}, &nop)

	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(context.Background())
	if err != nil {
		t.Fatalf("failed to load application: %v", err)
	}
	if updated.SyncStatus != models.OutOfSync {
		t.Errorf("expected SyncStatus %q, got %q", models.OutOfSync, updated.SyncStatus)
	}
	if updated.HealthStatus != models.Unhealthy {
		t.Errorf("expected HealthStatus %q, got %q", models.Unhealthy, updated.HealthStatus)
	}
	if updated.LastSyncedAt != nil {
		t.Error("expected LastSyncedAt to remain nil on failure")
	}
}

func TestUpdateApplicationStatus_AppliesUpdate(t *testing.T) {
	setupDeployTestEnv(t)
	app := seedDeployApp(t, models.UnknownSync)
	nop := zerolog.Nop()

	err := updateApplicationStatus(context.Background(), app.Id, models.Application{
		SyncStatus: models.Synced,
	}, &nop)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(context.Background())
	if err != nil {
		t.Fatalf("failed to load application: %v", err)
	}
	if updated.SyncStatus != models.Synced {
		t.Errorf("expected SyncStatus %q, got %q", models.Synced, updated.SyncStatus)
	}
}

func TestUpdateApplicationStatus_UnknownApp_NoError(t *testing.T) {
	setupDeployTestEnv(t)
	nop := zerolog.Nop()

	// Updating a non-existent id affects zero rows but is not an error.
	err := updateApplicationStatus(context.Background(), "does-not-exist", models.Application{
		SyncStatus: models.Synced,
	}, &nop)
	if err != nil {
		t.Fatalf("expected no error for unknown app, got %v", err)
	}
}
