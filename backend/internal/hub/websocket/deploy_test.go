package websocket

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/applicationevents"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/deployments"
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
	if err := testDB.AutoMigrate(&models.Repository{}, &models.Application{}, &models.Notification{}, &models.ApplicationEvent{}, &models.ApplicationDeployment{}, &models.ApplicationHealthChange{}); err != nil {
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

// expectAsyncNotification creates an observable completion barrier for the
// fire-and-forget notification goroutine. The invalid provider fails locally
// and sets the notification status after its final database access.
func expectAsyncNotification(t *testing.T) {
	t.Helper()
	t.Cleanup(notificationBarrier(t))
}

func notificationBarrier(t *testing.T) (wait func()) {
	t.Helper()
	notification := models.Notification{
		Name:            crypto.EncryptedString("async notification barrier"),
		Enabled:         true,
		AllApplications: true,
		Events:          models.NotificationEvents,
		Status:          models.NotificationStatusUnknown,
		Type:            models.NotificationType("test-invalid"),
		Config:          crypto.EncryptedString("{}"),
	}
	if err := db.DB.Select("*").Create(&notification).Error; err != nil {
		t.Fatalf("create notification barrier: %v", err)
	}
	return func() {
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			got, err := gorm.G[models.Notification](db.DB).
				Select("status").
				Where("id = ?", notification.Id).
				First(context.Background())
			if err == nil && got.Status == models.NotificationStatusError {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Error("async notification did not finish")
	}
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
	expectAsyncNotification(t)

	handleDeployResult(t.Context(), "agent-1", &messages.DeployResult{ApplicationId: app.Id, Success: true}, &nop)

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
	if updated.LastSyncSource == nil || *updated.LastSyncSource != "" {
		t.Errorf("expected empty LastSyncSource for a result without an event, got %v", updated.LastSyncSource)
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
	expectAsyncNotification(t)
	handleDeployResult(t.Context(), "agent-1", &messages.DeployResult{RequestId: requestID, ApplicationId: app.Id, Success: true}, &nop)
	event, err := gorm.G[models.ApplicationEvent](db.DB).Where("request_id = ?", requestID).First(t.Context())
	if err != nil || event.Status != models.ApplicationEventSucceeded {
		t.Fatalf("event=%+v err=%v", event, err)
	}
	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(t.Context())
	if err != nil {
		t.Fatalf("load application: %v", err)
	}
	if updated.LastSyncSource == nil || *updated.LastSyncSource != models.ApplicationEventSourceManual {
		t.Errorf("expected LastSyncSource %q, got %v", models.ApplicationEventSourceManual, updated.LastSyncSource)
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
	expectAsyncNotification(t)
	handleDeployResult(t.Context(), "agent-1", &messages.DeployResult{RequestId: "other-request", ApplicationId: app.Id, Success: true}, &nop)
	event, err := gorm.G[models.ApplicationEvent](db.DB).Where("request_id = ?", requestID).First(t.Context())
	if err != nil || event.Status != models.ApplicationEventRunning {
		t.Fatalf("event=%+v err=%v", event, err)
	}
}

func TestFailRunningDeploymentEventsForAgent(t *testing.T) {
	setupDeployTestEnv(t)
	app := seedAppForAgent(t, "agent-1")
	other := seedAppForAgent(t, "agent-2")
	tests := []struct {
		requestID     string
		applicationID string
		eventType     models.ApplicationEventType
		want          models.ApplicationEventStatus
	}{
		{"deployment", app.Id, models.ApplicationEventDeployment, models.ApplicationEventFailed},
		{"commit-sync", app.Id, models.ApplicationEventCommitSync, models.ApplicationEventRunning},
		{"image-update", app.Id, models.ApplicationEventImageUpdate, models.ApplicationEventRunning},
		{"other-agent", other.Id, models.ApplicationEventDeployment, models.ApplicationEventRunning},
	}
	for _, test := range tests {
		requestID := test.requestID
		if _, err := applicationevents.Start(t.Context(), applicationevents.Params{
			ApplicationID: test.applicationID,
			RequestID:     &requestID,
			Type:          test.eventType,
			Source:        models.ApplicationEventSourceManual,
		}); err != nil {
			t.Fatalf("start %s event: %v", test.requestID, err)
		}
	}

	nop := zerolog.Nop()
	failRunningDeploymentEventsForAgent(t.Context(), "agent-1", &nop)

	for _, test := range tests {
		event, err := gorm.G[models.ApplicationEvent](db.DB).
			Where("request_id = ?", test.requestID).
			First(t.Context())
		if err != nil {
			t.Fatalf("load %s event: %v", test.requestID, err)
		}
		if event.Status != test.want {
			t.Errorf("%s status = %q, want %q", test.requestID, event.Status, test.want)
		}
	}
}

func TestHandleDeployResult_Failure_SetsOutOfSync(t *testing.T) {
	setupDeployTestEnv(t)
	app := seedDeployApp(t, models.Syncing)
	nop := zerolog.Nop()
	expectAsyncNotification(t)

	handleDeployResult(t.Context(), "agent-1", &messages.DeployResult{
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
	if updated.HealthStatus != models.UnknownHealth {
		t.Errorf("expected HealthStatus to stay %q, got %q", models.UnknownHealth, updated.HealthStatus)
	}
	if updated.LastSyncedAt != nil {
		t.Error("expected LastSyncedAt to remain nil on failure")
	}
}

func TestUpdateApplicationStatus_AppliesUpdate(t *testing.T) {
	setupDeployTestEnv(t)
	app := seedDeployApp(t, models.UnknownSync)
	nop := zerolog.Nop()

	err := updateApplicationStatus(context.Background(), app.AgentId, app.Id, models.Application{
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
	err := updateApplicationStatus(context.Background(), "agent-1", "does-not-exist", models.Application{
		SyncStatus: models.Synced,
	}, &nop)
	if err != nil {
		t.Fatalf("expected no error for unknown app, got %v", err)
	}
}

func TestHandleDeployResult_OtherAgentsApplicationIsIgnored(t *testing.T) {
	setupDeployTestEnv(t)
	app := seedDeployApp(t, models.Syncing)
	requestID := "foreign-request"
	if _, err := applicationevents.Start(t.Context(), applicationevents.Params{
		ApplicationID: app.Id,
		RequestID:     &requestID,
		Type:          models.ApplicationEventDeployment,
		Source:        models.ApplicationEventSourceManual,
	}); err != nil {
		t.Fatalf("start event: %v", err)
	}
	nop := zerolog.Nop()

	for _, success := range []bool{true, false} {
		handleDeployResult(t.Context(), "agent-2", &messages.DeployResult{RequestId: requestID, ApplicationId: app.Id, Success: success, ErrorMessage: "boom"}, &nop)
	}

	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(t.Context())
	if err != nil {
		t.Fatalf("load application: %v", err)
	}
	if updated.SyncStatus != models.Syncing {
		t.Errorf("expected SyncStatus %q, got %q", models.Syncing, updated.SyncStatus)
	}
	event, err := gorm.G[models.ApplicationEvent](db.DB).Where("request_id = ?", requestID).First(t.Context())
	if err != nil || event.Status != models.ApplicationEventRunning {
		t.Fatalf("event=%+v err=%v", event, err)
	}
}

func TestHandleDeployResult_RecordsDeployment(t *testing.T) {
	setupDeployTestEnv(t)
	app := seedDeployApp(t, models.Syncing)
	nop := zerolog.Nop()
	expectAsyncNotification(t)
	if _, err := deployments.Start(t.Context(), &app, "services: {}\n", "req-1"); err != nil {
		t.Fatalf("start deployment: %v", err)
	}

	handleDeployResult(t.Context(), "agent-1", &messages.DeployResult{
		RequestId:     "req-1",
		ApplicationId: app.Id,
		Success:       true,
		Images:        []*messages.ServiceImage{{Service: "web", ImageId: "sha256:web", PinnedRef: "nginx@sha256:web"}},
	}, &nop)

	recorded, err := gorm.G[models.ApplicationDeployment](db.DB).Where("application_id = ?", app.Id).First(t.Context())
	if err != nil {
		t.Fatalf("load deployment: %v", err)
	}
	if recorded.Status != models.DeploymentSucceeded {
		t.Errorf("deployment status = %q, want succeeded", recorded.Status)
	}
	if len(recorded.Images) != 1 || recorded.Images[0].PinnedRef != "nginx@sha256:web" {
		t.Errorf("deployment images = %v", recorded.Images)
	}
	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(t.Context())
	if err != nil {
		t.Fatalf("load application: %v", err)
	}
	if updated.CurrentDeploymentId == nil || *updated.CurrentDeploymentId != recorded.Id {
		t.Errorf("current deployment = %v, want %q", updated.CurrentDeploymentId, recorded.Id)
	}
}

func TestHandleDeployResult_FailureDiscardsDeployment(t *testing.T) {
	setupDeployTestEnv(t)
	app := seedDeployApp(t, models.Syncing)
	nop := zerolog.Nop()
	expectAsyncNotification(t)
	if _, err := deployments.Start(t.Context(), &app, "services: {}\n", "req-1"); err != nil {
		t.Fatalf("start deployment: %v", err)
	}

	handleDeployResult(t.Context(), "agent-1", &messages.DeployResult{
		RequestId:     "req-1",
		ApplicationId: app.Id,
		ErrorMessage:  "compose up failed",
	}, &nop)

	count, err := gorm.G[models.ApplicationDeployment](db.DB).Where("application_id = ?", app.Id).Count(t.Context(), "*")
	if err != nil {
		t.Fatalf("count deployments: %v", err)
	}
	if count != 0 {
		t.Errorf("expected the failed deployment to be discarded, got %d", count)
	}
}
