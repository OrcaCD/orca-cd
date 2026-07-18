package websocket

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/applicationevents"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/notifications/provider"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
)

func setupImagePollTestEnv(t *testing.T) {
	t.Helper()
	runNotificationsSynchronously(t)
	setupHandlerTestEnv(t)
	if err := db.DB.AutoMigrate(&models.Repository{}, &models.Application{}, &models.Notification{}, &models.ApplicationEvent{}); err != nil {
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

const testHTTPNotificationType models.NotificationType = "test-http-image-poll"

type passthroughNotificationProvider struct{}

func (passthroughNotificationProvider) BuildShoutrrrUrls(rawConfig string) ([]string, error) {
	return []string{rawConfig}, nil
}

func registerTestHTTPNotificationProvider(t *testing.T) {
	t.Helper()
	provider.Register(testHTTPNotificationType, passthroughNotificationProvider{})
}

type capturedNotificationRequest struct {
	Body string
}

func newImagePollNotificationCaptureServer(t *testing.T) (*httptest.Server, <-chan capturedNotificationRequest) {
	t.Helper()

	requests := make(chan capturedNotificationRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read notification request body: %v", err)
		}
		requests <- capturedNotificationRequest{Body: string(body)}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	return server, requests
}

func genericNotificationURL(t *testing.T, serverURL string) string {
	t.Helper()

	parsed, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("failed to parse test server URL %q: %v", serverURL, err)
	}
	parsed.Scheme = "generic"
	query := parsed.Query()
	query.Set("disabletls", "yes")
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func assertCapturedNotificationRequest(t *testing.T, requests <-chan capturedNotificationRequest, wantBody string) {
	t.Helper()

	select {
	case request := <-requests:
		if request.Body != wantBody {
			t.Fatalf("expected notification body %q, got %q", wantBody, request.Body)
		}
	default:
		t.Fatal("expected HTTP server to receive notification request")
	}
}

func assertNoNotificationRequest(t *testing.T, requests <-chan capturedNotificationRequest) {
	t.Helper()

	select {
	case request := <-requests:
		t.Fatalf("expected no notification request, got body %q", request.Body)
	default:
	}
}

func createTestNotification(t *testing.T, rawConfig string) {
	t.Helper()

	notification := &models.Notification{
		Name:            crypto.EncryptedString("test-notification"),
		Enabled:         true,
		EnableByDefault: true,
		Status:          models.NotificationStatusUnknown,
		Type:            testHTTPNotificationType,
		Config:          crypto.EncryptedString(rawConfig),
	}
	if err := db.DB.Create(notification).Error; err != nil {
		t.Fatalf("failed to create notification: %v", err)
	}
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

func TestHandlePullImagesResult_WrongAgent(t *testing.T) {
	setupImagePollTestEnv(t)
	registerTestHTTPNotificationProvider(t)
	log := testLogger()

	owner := createTestAgent(t, "key-ipr-owner")
	app := createTestApplication(t, owner.Id)

	server, requests := newImagePollNotificationCaptureServer(t)
	createTestNotification(t, genericNotificationURL(t, server.URL))

	// A different agent sends a result claiming to own this application.
	attacker := createTestAgent(t, "key-ipr-attacker")
	attackerClient := &Client{Id: attacker.Id, Send: make(chan *messages.ServerMessage, 1)}

	handlePullImagesResult(attackerClient, &messages.PullImagesResult{
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
	assertNoNotificationRequest(t, requests)
}

func TestHandlePullImagesResult_Success_SendsNotification(t *testing.T) {
	setupImagePollTestEnv(t)
	registerTestHTTPNotificationProvider(t)
	log := testLogger()

	server, requests := newImagePollNotificationCaptureServer(t)
	createTestNotification(t, genericNotificationURL(t, server.URL))

	agent := createTestAgent(t, "key-ipr-notify-success")
	app := createTestApplication(t, agent.Id)
	client := &Client{Id: agent.Id, Send: make(chan *messages.ServerMessage, 1)}

	handlePullImagesResult(client, &messages.PullImagesResult{
		ApplicationId: app.Id,
		Success:       true,
		ImagesUpdated: true,
	}, &log)

	assertCapturedNotificationRequest(t, requests, "Success: image update succeeded for test-app")
}

func TestHandlePullImagesResult_Failure_SendsNotification(t *testing.T) {
	setupImagePollTestEnv(t)
	registerTestHTTPNotificationProvider(t)
	log := testLogger()

	server, requests := newImagePollNotificationCaptureServer(t)
	createTestNotification(t, genericNotificationURL(t, server.URL))

	agent := createTestAgent(t, "key-ipr-notify-failure")
	app := createTestApplication(t, agent.Id)
	client := &Client{Id: agent.Id, Send: make(chan *messages.ServerMessage, 1)}

	handlePullImagesResult(client, &messages.PullImagesResult{
		ApplicationId: app.Id,
		Success:       false,
		ErrorMessage:  "registry timeout",
	}, &log)

	assertCapturedNotificationRequest(t, requests, "Error: image update failed for test-app")
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

	handlePullImagesResult(client, &messages.PullImagesResult{
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
}

func TestHandlePullImagesResult_ExplicitRequestNoUpdates_CompletesNoChange(t *testing.T) {
	setupImagePollTestEnv(t)
	log := testLogger()

	agent := createTestAgent(t, "key-ipr-evt-2")
	app := createTestApplication(t, agent.Id)
	client := &Client{Id: agent.Id, Send: make(chan *messages.ServerMessage, 1)}
	startImageUpdateEvent(t, app.Id, "image-req-2")

	handlePullImagesResult(client, &messages.PullImagesResult{
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

	handlePullImagesResult(client, &messages.PullImagesResult{
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

	handlePullImagesResult(client, &messages.PullImagesResult{
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

func TestHandlePullImagesResult_PeriodicFailure_RecordsTerminalPollingEvent(t *testing.T) {
	setupImagePollTestEnv(t)
	log := testLogger()

	agent := createTestAgent(t, "key-ipr-evt-5")
	app := createTestApplication(t, agent.Id)
	client := &Client{Id: agent.Id, Send: make(chan *messages.ServerMessage, 1)}

	handlePullImagesResult(client, &messages.PullImagesResult{
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
