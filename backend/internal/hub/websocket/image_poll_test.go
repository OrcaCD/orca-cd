package websocket

import (
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/applicationevents"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"gorm.io/gorm"
)

func setupImagePollTestEnv(t *testing.T) {
	t.Helper()
	setupHandlerTestEnv(t)
	if err := db.DB.AutoMigrate(&models.Repository{}, &models.Application{}, &models.Notification{}, &models.ApplicationEvent{}, &models.ApplicationDeployment{}); err != nil {
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
	expectAsyncNotification(t)

	handlePullImagesResult(t.Context(), client, &messages.PullImagesResult{
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
	if updated.LastSyncSource == nil || *updated.LastSyncSource != models.ApplicationEventSourceImagePolling {
		t.Errorf("expected LastSyncSource %q, got %v", models.ApplicationEventSourceImagePolling, updated.LastSyncSource)
	}
}

func TestHandlePullImagesResult_WrongAgent(t *testing.T) {
	setupImagePollTestEnv(t)
	log := testLogger()

	owner := createTestAgent(t, "key-ipr-owner")
	app := createTestApplication(t, owner.Id)

	// A different agent sends a result claiming to own this application.
	attacker := createTestAgent(t, "key-ipr-attacker")
	attackerClient := &Client{Id: attacker.Id, Send: make(chan *messages.ServerMessage, 1)}

	handlePullImagesResult(t.Context(), attackerClient, &messages.PullImagesResult{
		ApplicationId: app.Id,
		Success:       true,
		ImagesUpdated: true,
	}, &log)

	var unchanged models.Application
	if err := db.DB.First(&unchanged, "id = ?", app.Id).Error; err != nil {
		t.Fatalf("failed to query application: %v", err)
	}
	if unchanged.SyncStatus != models.UnknownSync {
		t.Errorf("expected SyncStatus to remain %q, got %q", models.UnknownSync, unchanged.SyncStatus)
	}
	if unchanged.LastSyncedAt != nil {
		t.Errorf("expected LastSyncedAt to remain nil, got %v", unchanged.LastSyncedAt)
	}
}

func startImageUpdateEvent(t *testing.T, applicationID, requestID string) {
	t.Helper()
	if _, err := applicationevents.Start(t.Context(), applicationevents.Params{
		ApplicationID: applicationID,
		RequestID:     &requestID,
		Type:          models.ApplicationEventImageUpdate,
		Source:        models.ApplicationEventSourceImageWebhook,
	}); err != nil {
		t.Fatalf("failed to start image update event: %v", err)
	}
}

func loadImageEvents(t *testing.T, applicationID string) []models.ApplicationEvent {
	t.Helper()
	var events []models.ApplicationEvent
	if err := db.DB.Where("application_id = ?", applicationID).Find(&events).Error; err != nil {
		t.Fatalf("failed to load events: %v", err)
	}
	return events
}

func TestHandlePullImagesResult_ExplicitRequestImagesUpdated_CompletesSucceeded(t *testing.T) {
	setupImagePollTestEnv(t)
	log := testLogger()

	agent := createTestAgent(t, "key-ipr-evt-1")
	app := createTestApplication(t, agent.Id)
	client := &Client{Id: agent.Id, Send: make(chan *messages.ServerMessage, 1)}
	startImageUpdateEvent(t, app.Id, "image-req-1")
	expectAsyncNotification(t)

	handlePullImagesResult(t.Context(), client, &messages.PullImagesResult{
		RequestId:     "image-req-1",
		ApplicationId: app.Id,
		Success:       true,
		ImagesUpdated: true,
	}, &log)

	events := loadImageEvents(t, app.Id)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Status != models.ApplicationEventSucceeded || events[0].CompletedAt == nil {
		t.Fatalf("expected succeeded completed event, got %+v", events[0])
	}

	var updated models.Application
	if err := db.DB.First(&updated, "id = ?", app.Id).Error; err != nil {
		t.Fatalf("failed to query application: %v", err)
	}
	if updated.LastSyncSource == nil || *updated.LastSyncSource != models.ApplicationEventSourceImageWebhook {
		t.Errorf("expected LastSyncSource %q, got %v", models.ApplicationEventSourceImageWebhook, updated.LastSyncSource)
	}
}

func TestHandlePullImagesResult_ExplicitRequestNoUpdates_CompletesNoChange(t *testing.T) {
	setupImagePollTestEnv(t)
	log := testLogger()

	agent := createTestAgent(t, "key-ipr-evt-2")
	app := createTestApplication(t, agent.Id)
	client := &Client{Id: agent.Id, Send: make(chan *messages.ServerMessage, 1)}
	startImageUpdateEvent(t, app.Id, "image-req-2")
	expectAsyncNotification(t)

	handlePullImagesResult(t.Context(), client, &messages.PullImagesResult{
		RequestId:     "image-req-2",
		ApplicationId: app.Id,
		Success:       true,
		ImagesUpdated: false,
	}, &log)

	events := loadImageEvents(t, app.Id)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Status != models.ApplicationEventNoChange {
		t.Fatalf("expected no_change event, got %+v", events[0])
	}
}

func TestHandlePullImagesResult_PeriodicNoOpSuccess_RecordsNothing(t *testing.T) {
	setupImagePollTestEnv(t)
	log := testLogger()

	agent := createTestAgent(t, "key-ipr-evt-3")
	app := createTestApplication(t, agent.Id)
	client := &Client{Id: agent.Id, Send: make(chan *messages.ServerMessage, 1)}
	expectAsyncNotification(t)

	handlePullImagesResult(t.Context(), client, &messages.PullImagesResult{
		RequestId:     "unsolicited-poll",
		ApplicationId: app.Id,
		Success:       true,
		ImagesUpdated: false,
	}, &log)

	if events := loadImageEvents(t, app.Id); len(events) != 0 {
		t.Fatalf("expected no events for a no-op periodic poll, got %d", len(events))
	}
}

func TestHandlePullImagesResult_PeriodicUpdate_RecordsTerminalPollingEvent(t *testing.T) {
	setupImagePollTestEnv(t)
	log := testLogger()

	agent := createTestAgent(t, "key-ipr-evt-4")
	app := createTestApplication(t, agent.Id)
	client := &Client{Id: agent.Id, Send: make(chan *messages.ServerMessage, 1)}
	expectAsyncNotification(t)

	handlePullImagesResult(t.Context(), client, &messages.PullImagesResult{
		RequestId:     "periodic-updated",
		ApplicationId: app.Id,
		Success:       true,
		ImagesUpdated: true,
	}, &log)

	events := loadImageEvents(t, app.Id)
	if len(events) != 1 {
		t.Fatalf("expected 1 event for periodic update, got %d", len(events))
	}
	if events[0].Status != models.ApplicationEventSucceeded ||
		events[0].Source != models.ApplicationEventSourceImagePolling ||
		events[0].Type != models.ApplicationEventImageUpdate {
		t.Fatalf("unexpected periodic update event: %+v", events[0])
	}
}

func TestHandlePullImagesResult_UpdateRecordsDeployment(t *testing.T) {
	for _, requestID := range []string{"image-req", ""} {
		t.Run("request "+requestID, func(t *testing.T) {
			setupImagePollTestEnv(t)
			log := testLogger()

			agent := createTestAgent(t, "key-ipr-deploy-"+requestID)
			app := createTestApplication(t, agent.Id)
			client := &Client{Id: agent.Id, Send: make(chan *messages.ServerMessage, 1)}
			expectAsyncNotification(t)

			handlePullImagesResult(t.Context(), client, &messages.PullImagesResult{
				RequestId:     requestID,
				ApplicationId: app.Id,
				Success:       true,
				ImagesUpdated: true,
				Images:        []*messages.ServiceImage{{Service: "web", ImageId: "sha256:new"}},
			}, &log)

			recorded, err := gorm.G[models.ApplicationDeployment](db.DB).Where("application_id = ?", app.Id).First(t.Context())
			if err != nil {
				t.Fatalf("load deployment: %v", err)
			}
			if len(recorded.Images) != 1 || recorded.Images[0].ImageId != "sha256:new" {
				t.Errorf("deployment images = %v", recorded.Images)
			}
			events := loadImageEvents(t, app.Id)
			if len(events) != 1 || events[0].DeploymentId == nil || *events[0].DeploymentId != recorded.Id {
				t.Errorf("expected the image update event to link the deployment, got %+v", events)
			}
		})
	}
}

func TestHandlePullImagesResult_NoUpdateRecordsNoDeployment(t *testing.T) {
	setupImagePollTestEnv(t)
	log := testLogger()

	agent := createTestAgent(t, "key-ipr-no-deploy")
	app := createTestApplication(t, agent.Id)
	client := &Client{Id: agent.Id, Send: make(chan *messages.ServerMessage, 1)}
	expectAsyncNotification(t)

	handlePullImagesResult(t.Context(), client, &messages.PullImagesResult{
		RequestId:     "image-req",
		ApplicationId: app.Id,
		Success:       true,
	}, &log)

	count, err := gorm.G[models.ApplicationDeployment](db.DB).Where("application_id = ?", app.Id).Count(t.Context(), "*")
	if err != nil {
		t.Fatalf("count deployments: %v", err)
	}
	if count != 0 {
		t.Errorf("expected no deployment without an image update, got %d", count)
	}
}

func TestHandlePullImagesResult_PeriodicFailure_RecordsTerminalPollingEvent(t *testing.T) {
	setupImagePollTestEnv(t)
	log := testLogger()

	agent := createTestAgent(t, "key-ipr-evt-5")
	app := createTestApplication(t, agent.Id)
	client := &Client{Id: agent.Id, Send: make(chan *messages.ServerMessage, 1)}
	expectAsyncNotification(t)

	handlePullImagesResult(t.Context(), client, &messages.PullImagesResult{
		RequestId:     "periodic-failed",
		ApplicationId: app.Id,
		Success:       false,
		ErrorMessage:  "registry timeout",
	}, &log)

	events := loadImageEvents(t, app.Id)
	if len(events) != 1 {
		t.Fatalf("expected 1 event for periodic failure, got %d", len(events))
	}
	if events[0].Status != models.ApplicationEventFailed ||
		events[0].Source != models.ApplicationEventSourceImagePolling ||
		events[0].ErrorMessage == nil || *events[0].ErrorMessage != "registry timeout" {
		t.Fatalf("unexpected periodic failure event: %+v", events[0])
	}
}

func handleAwaitingNotification(t *testing.T, client *Client, r *messages.PullImagesResult) {
	t.Helper()
	log := testLogger()
	wait := notificationBarrier(t)
	handlePullImagesResult(t.Context(), client, r, &log)
	wait()
}

func TestHandlePullImagesResult_RepeatedPeriodicFailure_RecordsOnce(t *testing.T) {
	setupImagePollTestEnv(t)
	log := testLogger()

	agent := createTestAgent(t, "key-ipr-dedupe-1")
	app := createTestApplication(t, agent.Id)
	client := &Client{Id: agent.Id, Send: make(chan *messages.ServerMessage, 1)}

	failure := &messages.PullImagesResult{
		ApplicationId: app.Id,
		Success:       false,
		ErrorMessage:  "image reg.example.com/x:main: unauthorized",
	}
	handleAwaitingNotification(t, client, failure)
	handlePullImagesResult(t.Context(), client, failure, &log)

	if events := loadImageEvents(t, app.Id); len(events) != 1 {
		t.Fatalf("expected the repeated failure to create 1 event, got %d", len(events))
	}
}

func TestHandlePullImagesResult_PeriodicFailureWithDifferentError_RecordsAgain(t *testing.T) {
	setupImagePollTestEnv(t)

	agent := createTestAgent(t, "key-ipr-dedupe-2")
	app := createTestApplication(t, agent.Id)
	client := &Client{Id: agent.Id, Send: make(chan *messages.ServerMessage, 1)}

	for _, msg := range []string{"unauthorized", "registry timeout"} {
		handleAwaitingNotification(t, client, &messages.PullImagesResult{
			ApplicationId: app.Id,
			Success:       false,
			ErrorMessage:  msg,
		})
	}

	if events := loadImageEvents(t, app.Id); len(events) != 2 {
		t.Fatalf("expected a changed error to create a new event, got %d events", len(events))
	}
}

func TestHandlePullImagesResult_FailureAfterRecovery_RecordsAgain(t *testing.T) {
	setupImagePollTestEnv(t)

	agent := createTestAgent(t, "key-ipr-dedupe-3")
	app := createTestApplication(t, agent.Id)
	client := &Client{Id: agent.Id, Send: make(chan *messages.ServerMessage, 1)}

	failure := &messages.PullImagesResult{ApplicationId: app.Id, Success: false, ErrorMessage: "unauthorized"}
	handleAwaitingNotification(t, client, failure)
	handleAwaitingNotification(t, client, &messages.PullImagesResult{ApplicationId: app.Id, Success: true})
	handleAwaitingNotification(t, client, failure)

	if events := loadImageEvents(t, app.Id); len(events) != 2 {
		t.Fatalf("expected a failure after recovery to be reported again, got %d events", len(events))
	}
}

func TestHandlePullImagesResult_ExplicitFailure_AlwaysReports(t *testing.T) {
	setupImagePollTestEnv(t)

	agent := createTestAgent(t, "key-ipr-dedupe-4")
	app := createTestApplication(t, agent.Id)
	client := &Client{Id: agent.Id, Send: make(chan *messages.ServerMessage, 1)}

	handleAwaitingNotification(t, client, &messages.PullImagesResult{
		ApplicationId: app.Id,
		Success:       false,
		ErrorMessage:  "unauthorized",
	})

	startImageUpdateEvent(t, app.Id, "image-req-dedupe")
	handleAwaitingNotification(t, client, &messages.PullImagesResult{
		RequestId:     "image-req-dedupe",
		ApplicationId: app.Id,
		Success:       false,
		ErrorMessage:  "unauthorized",
	})

	events := loadImageEvents(t, app.Id)
	if len(events) != 2 {
		t.Fatalf("expected the explicit trigger to report despite the earlier failure, got %d events", len(events))
	}
	for _, e := range events {
		if e.Status != models.ApplicationEventFailed {
			t.Errorf("expected failed event, got %+v", e)
		}
	}
}

func TestHandlePullImagesResult_Failure(t *testing.T) {
	setupImagePollTestEnv(t)
	log := testLogger()

	agent := createTestAgent(t, "key-ipr-2")
	app := createTestApplication(t, agent.Id)
	client := &Client{Id: agent.Id, Send: make(chan *messages.ServerMessage, 1)}
	expectAsyncNotification(t)

	handlePullImagesResult(t.Context(), client, &messages.PullImagesResult{
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
