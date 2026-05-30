package websocket

import (
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
)

func setupImagePollTestEnv(t *testing.T) {
	t.Helper()
	setupHandlerTestEnv(t)
	if err := db.DB.AutoMigrate(&models.Repository{}, &models.Application{}); err != nil {
		t.Fatalf("failed to migrate Application: %v", err)
	}
	sse.DefaultBroker = sse.NewBroker(&zerolog_disabled)
}

var zerolog_disabled = testLogger()

func createTestApplication(t *testing.T, agentID string) *models.Application {
	t.Helper()
	repo := &models.Repository{
		Url:      "https://example.com/repo.git",
		SyncType: "polling",
	}
	if err := db.DB.Create(repo).Error; err != nil {
		t.Fatalf("failed to create repository: %v", err)
	}
	app := &models.Application{
		Name:         crypto.EncryptedString("test-app"),
		RepositoryId: repo.Id,
		AgentId:      agentID,
		SyncStatus:   models.UnknownSync,
		HealthStatus: models.UnknownHealth,
		Branch:       "main",
		ComposeFile:  crypto.EncryptedString(""),
	}
	if err := db.DB.Create(app).Error; err != nil {
		t.Fatalf("failed to create application: %v", err)
	}
	return app
}

func TestHandlePullImagesResult_Success(t *testing.T) {
	setupImagePollTestEnv(t)
	log := testLogger()

	agent := createTestAgent(t, "key-ipr-1")
	app := createTestApplication(t, agent.Id)
	client := &Client{Id: agent.Id, Send: make(chan *messages.ServerMessage, 1)}

	before := time.Now().Truncate(time.Second)

	handlePullImagesResult(client, &messages.PullImagesResult{
		ApplicationId: app.Id,
		Success:       true,
		ImagesUpdated: true,
	}, &log)

	var updated models.Application
	if err := db.DB.First(&updated, "id = ?", app.Id).Error; err != nil {
		t.Fatalf("failed to query application: %v", err)
	}
	if updated.SyncStatus != models.Synced {
		t.Errorf("expected SyncStatus %q, got %q", models.Synced, updated.SyncStatus)
	}
	if updated.LastSyncedAt == nil {
		t.Fatal("expected LastSyncedAt to be set")
	}
	if updated.LastSyncedAt.Before(before) {
		t.Errorf("LastSyncedAt %v is before test start %v", updated.LastSyncedAt, before)
	}
}

func TestHandlePullImagesResult_Failure(t *testing.T) {
	setupImagePollTestEnv(t)
	log := testLogger()

	agent := createTestAgent(t, "key-ipr-2")
	app := createTestApplication(t, agent.Id)
	client := &Client{Id: agent.Id, Send: make(chan *messages.ServerMessage, 1)}

	handlePullImagesResult(client, &messages.PullImagesResult{
		ApplicationId: app.Id,
		Success:       false,
		ErrorMessage:  "registry timeout",
	}, &log)

	var updated models.Application
	if err := db.DB.First(&updated, "id = ?", app.Id).Error; err != nil {
		t.Fatalf("failed to query application: %v", err)
	}
	if updated.SyncStatus != models.OutOfSync {
		t.Errorf("expected SyncStatus %q, got %q", models.OutOfSync, updated.SyncStatus)
	}
	if updated.LastSyncedAt != nil {
		t.Errorf("expected LastSyncedAt to remain nil on failure, got %v", updated.LastSyncedAt)
	}
}
