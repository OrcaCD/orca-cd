package application_deployer

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

// mockSender records what the deployer sends and reports a configurable
// connected/not-connected result, so tests need no real hub or agent.
type mockSender struct {
	connected bool
	agentID   string
	sent      *messages.ServerMessage
}

func (m *mockSender) Send(agentID string, msg *messages.ServerMessage) bool {
	m.agentID = agentID
	m.sent = msg
	return m.connected
}

func setupTestDB(t *testing.T) {
	t.Helper()

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
	if err := testDB.AutoMigrate(&models.Agent{}, &models.Repository{}, &models.Application{}, &models.AuditLog{}, &models.ApplicationEvent{}); err != nil {
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

	if err := crypto.Init("test-secret-that-is-long-enough-32chars"); err != nil {
		t.Fatalf("failed to init crypto: %v", err)
	}
}

func seedApp(t *testing.T) models.Application {
	t.Helper()
	app := models.Application{
		Name:         crypto.EncryptedString("test-app"),
		AgentId:      "agent-1",
		SyncStatus:   models.UnknownSync,
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

func TestTriggerApplicationDeploy_AgentConnected_MarksSyncingAndSends(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t)
	nop := zerolog.Nop()

	sender := &mockSender{connected: true}
	d := NewApplicationDeployer(sender, &nop)

	if err := d.TriggerApplicationDeploy(context.Background(), &app, "version: '3.9'\n", "request-connected"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sender.agentID != app.AgentId {
		t.Errorf("expected send to agent %q, got %q", app.AgentId, sender.agentID)
	}
	if sender.sent.GetDeployRequest() == nil {
		t.Fatal("expected a DeployRequest to be sent")
	}
	if got := sender.sent.GetDeployRequest().ApplicationId; got != app.Id {
		t.Errorf("expected DeployRequest for app %q, got %q", app.Id, got)
	}
	if got := sender.sent.GetDeployRequest().RequestId; got != "request-connected" {
		t.Errorf("expected request id %q, got %q", "request-connected", got)
	}

	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(context.Background())
	if err != nil {
		t.Fatalf("failed to load application: %v", err)
	}
	if updated.SyncStatus != models.Syncing {
		t.Errorf("expected SyncStatus %q, got %q", models.Syncing, updated.SyncStatus)
	}
}

func TestTriggerApplicationDeploy_AgentNotConnected_MarksOutOfSync(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t)
	nop := zerolog.Nop()

	sender := &mockSender{connected: false}
	d := NewApplicationDeployer(sender, &nop)

	requestID := "request-offline"
	if _, err := applicationevents.Start(t.Context(), applicationevents.Params{
		ApplicationID: app.Id,
		RequestID:     &requestID,
		Type:          models.ApplicationEventDeployment,
		Source:        models.ApplicationEventSourceManual,
	}); err != nil {
		t.Fatalf("start event: %v", err)
	}
	err := d.TriggerApplicationDeploy(context.Background(), &app, "version: '3.9'\n", requestID)
	if err == nil {
		t.Fatal("expected error when agent not connected")
	}

	updated, dbErr := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(context.Background())
	if dbErr != nil {
		t.Fatalf("failed to load application: %v", dbErr)
	}
	if updated.SyncStatus != models.OutOfSync {
		t.Errorf("expected SyncStatus %q, got %q", models.OutOfSync, updated.SyncStatus)
	}
	event, eventErr := gorm.G[models.ApplicationEvent](db.DB).Where("request_id = ?", requestID).First(t.Context())
	if eventErr != nil {
		t.Fatalf("load application event: %v", eventErr)
	}
	if event.Status != models.ApplicationEventFailed || event.CompletedAt == nil {
		t.Fatalf("offline event not failed: %+v", event)
	}
}
