package routes

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	hubnotifications "github.com/OrcaCD/orca-cd/internal/hub/notifications"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func setupTestDBWithNotifications(t *testing.T) {
	t.Helper()
	setupTestDB(t)
	if err := db.DB.AutoMigrate(&models.Repository{}, &models.Agent{}, &models.Application{}, &models.Notification{}); err != nil {
		t.Fatalf("failed to migrate Notification/Repository/Agent/Application: %v", err)
	}
}

func seedNotificationRepository(t *testing.T) models.Repository {
	t.Helper()

	repository := models.Repository{
		Name:       "Repo",
		Url:        "https://github.com/orcacd/notifications",
		Provider:   models.GitHub,
		AuthMethod: models.AuthMethodNone,
		SyncType:   models.SyncTypeManual,
		SyncStatus: models.SyncStatusUnknown,
		CreatedBy:  "user-1",
	}

	if err := db.DB.WithContext(t.Context()).Create(&repository).Error; err != nil {
		t.Fatalf("failed to seed repository: %v", err)
	}

	return repository
}

func seedNotificationAgent(t *testing.T) models.Agent {
	t.Helper()

	agent := models.Agent{
		Name:   crypto.EncryptedString("Notifications Agent"),
		KeyId:  crypto.EncryptedString("notifications-key"),
		Status: models.AgentStatusOffline,
	}

	if err := db.DB.WithContext(t.Context()).Create(&agent).Error; err != nil {
		t.Fatalf("failed to seed agent: %v", err)
	}

	return agent
}

func seedNotificationApplication(t *testing.T, repositoryId, agentId, name string) models.Application {
	t.Helper()

	application := models.Application{
		Name:                crypto.EncryptedString(name),
		RepositoryId:        repositoryId,
		AgentId:             agentId,
		SyncStatus:          models.UnknownSync,
		HealthStatus:        models.UnknownHealth,
		Branch:              "main",
		Commit:              "abc123",
		CommitMessage:       "seed commit",
		Path:                "compose.yaml",
		ComposeFile:         crypto.EncryptedString("services: {}"),
		PreviousComposeFile: crypto.EncryptedString(""),
	}

	if err := db.DB.WithContext(t.Context()).Select("*").Create(&application).Error; err != nil {
		t.Fatalf("failed to seed application: %v", err)
	}

	return application
}

func createNotificationRecord(t *testing.T, applicationIds []string) models.Notification {
	t.Helper()

	notification := models.Notification{
		Name:            crypto.EncryptedString("Initial Notification"),
		Enabled:         true,
		EnableByDefault: false,
		Status:          models.NotificationUnknownHealth,
		Type:            models.NotificationTypeDiscord,
		Config:          crypto.EncryptedString("discord://token@channel"),
	}

	if err := db.DB.WithContext(t.Context()).Select("*").Create(&notification).Error; err != nil {
		t.Fatalf("failed to create notification: %v", err)
	}

	if len(applicationIds) > 0 {
		applications, err := gorm.G[models.Application](db.DB).Where("id IN ?", applicationIds).Find(t.Context())
		if err != nil {
			t.Fatalf("failed to load applications for notification: %v", err)
		}
		if err := db.DB.Model(&notification).Association("Applications").Replace(applications); err != nil {
			t.Fatalf("failed to attach applications to notification: %v", err)
		}
	}

	return notification
}

func TestListNotificationsHandler_Empty(t *testing.T) {
	setupTestDBWithNotifications(t)

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/notifications", nil)

	ListNotificationsHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body []notificationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(body) != 0 {
		t.Fatalf("expected 0 notifications, got %d", len(body))
	}
}

func TestListNotificationsHandler_ReturnsNotifications(t *testing.T) {
	setupTestDBWithNotifications(t)

	repo := seedNotificationRepository(t)
	agent := seedNotificationAgent(t)
	appA := seedNotificationApplication(t, repo.Id, agent.Id, "App A")
	appB := seedNotificationApplication(t, repo.Id, agent.Id, "App B")
	createNotificationRecord(t, []string{appA.Id, appB.Id})
	createNotificationRecord(t, []string{appB.Id})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/notifications", nil)

	ListNotificationsHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body []notificationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(body) != 2 {
		t.Fatalf("expected 2 notifications, got %d", len(body))
	}
}

func TestCreateNotificationHandler_Success(t *testing.T) {
	setupTestDBWithNotifications(t)

	repo := seedNotificationRepository(t)
	agent := seedNotificationAgent(t)
	appA := seedNotificationApplication(t, repo.Id, agent.Id, "App A")
	appB := seedNotificationApplication(t, repo.Id, agent.Id, "App B")

	reqBody, _ := json.Marshal(map[string]any{
		"name":            "Deploy Alerts",
		"enabled":         true,
		"enableByDefault": false,
		"status":          "healthy",
		"type":            "discord",
		"config":          "discord://token@channel",
		"applicationIds":  []string{appA.Id, appB.Id},
	})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/notifications", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	CreateNotificationHandler(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var body notificationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if body.Name != "Deploy Alerts" {
		t.Fatalf("expected name %q, got %q", "Deploy Alerts", body.Name)
	}
	if body.Status != "healthy" {
		t.Fatalf("expected status %q, got %q", "healthy", body.Status)
	}
	if body.Type != "discord" {
		t.Fatalf("expected type %q, got %q", "discord", body.Type)
	}
	if len(body.ApplicationIds) != 2 {
		t.Fatalf("expected 2 applicationIds, got %d", len(body.ApplicationIds))
	}
	if !slices.Contains(body.ApplicationIds, appA.Id) || !slices.Contains(body.ApplicationIds, appB.Id) {
		t.Fatalf("response applicationIds missing expected ids: %v", body.ApplicationIds)
	}

	stored, err := gorm.G[models.Notification](db.DB).Preload("Applications", nil).Where("id = ?", body.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load stored notification: %v", err)
	}
	if stored.Config.String() != "discord://token@channel" {
		t.Fatalf("expected config to roundtrip, got %q", stored.Config.String())
	}
	if len(stored.Applications) != 2 {
		t.Fatalf("expected 2 associated applications, got %d", len(stored.Applications))
	}
}

func TestCreateNotificationHandler_InvalidRequest(t *testing.T) {
	setupTestDBWithNotifications(t)

	reqBody, _ := json.Marshal(map[string]any{})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/notifications", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	CreateNotificationHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateNotificationHandler_UnknownApplication(t *testing.T) {
	setupTestDBWithNotifications(t)

	reqBody, _ := json.Marshal(map[string]any{
		"name":           "Deploy Alerts",
		"status":         "healthy",
		"type":           "discord",
		"config":         "discord://token@channel",
		"applicationIds": []string{"missing-app"},
	})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/notifications", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	CreateNotificationHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateNotificationHandler_DiscordObjectConfigWithThreadID(t *testing.T) {
	setupTestDBWithNotifications(t)

	repo := seedNotificationRepository(t)
	agent := seedNotificationAgent(t)
	appA := seedNotificationApplication(t, repo.Id, agent.Id, "App A")

	reqBody, _ := json.Marshal(map[string]any{
		"name":   "Discord Alerts",
		"status": "healthy",
		"type":   "discord",
		"config": map[string]any{
			"token":     "token-abc",
			"webhookId": "123456789",
			"threadId":  "987654321",
		},
		"applicationIds": []string{appA.Id},
	})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/notifications", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	CreateNotificationHandler(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var body notificationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	stored, err := gorm.G[models.Notification](db.DB).Where("id = ?", body.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load notification: %v", err)
	}

	urls, err := hubnotifications.BuildShoutrrrURLs(stored.Type, stored.Config.String())
	if err != nil {
		t.Fatalf("BuildShoutrrrURLs() error: %v", err)
	}
	if len(urls) != 1 {
		t.Fatalf("expected 1 URL, got %d", len(urls))
	}
	if !strings.Contains(urls[0], "thread_id=987654321") {
		t.Fatalf("expected built URL to include thread_id, got %s", urls[0])
	}
}

func TestCreateNotificationHandler_InvalidDiscordObjectConfig(t *testing.T) {
	setupTestDBWithNotifications(t)

	reqBody, _ := json.Marshal(map[string]any{
		"name":   "Discord Alerts",
		"status": "healthy",
		"type":   "discord",
		"config": map[string]any{
			"webhookId": "123456789",
		},
	})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/notifications", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	CreateNotificationHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateNotificationHandler_Success(t *testing.T) {
	setupTestDBWithNotifications(t)

	repo := seedNotificationRepository(t)
	agent := seedNotificationAgent(t)
	appA := seedNotificationApplication(t, repo.Id, agent.Id, "App A")
	appB := seedNotificationApplication(t, repo.Id, agent.Id, "App B")
	notification := createNotificationRecord(t, []string{appA.Id})

	reqBody, _ := json.Marshal(map[string]any{
		"name":            "Updated Alerts",
		"enabled":         false,
		"enableByDefault": true,
		"status":          "unhealthy",
		"type":            "discord",
		"config":          "discord://new-token@channel",
		"applicationIds":  []string{appB.Id},
	})

	router := gin.New()
	router.PUT("/api/v1/notifications/:id", UpdateNotificationHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/notifications/"+notification.Id, bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body notificationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Name != "Updated Alerts" {
		t.Fatalf("expected name %q, got %q", "Updated Alerts", body.Name)
	}
	if body.Enabled {
		t.Fatal("expected enabled=false")
	}
	if !body.EnableByDefault {
		t.Fatal("expected enableByDefault=true")
	}
	if body.Status != "unhealthy" {
		t.Fatalf("expected status %q, got %q", "unhealthy", body.Status)
	}
	if len(body.ApplicationIds) != 1 || body.ApplicationIds[0] != appB.Id {
		t.Fatalf("expected applicationIds [%s], got %v", appB.Id, body.ApplicationIds)
	}

	stored, err := gorm.G[models.Notification](db.DB).Preload("Applications", nil).Where("id = ?", notification.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load updated notification: %v", err)
	}
	if stored.Name.String() != "Updated Alerts" {
		t.Fatalf("expected updated name, got %q", stored.Name.String())
	}
	if stored.Enabled {
		t.Fatal("expected stored enabled=false")
	}
	if !stored.EnableByDefault {
		t.Fatal("expected stored enableByDefault=true")
	}
	if len(stored.Applications) != 1 || stored.Applications[0].Id != appB.Id {
		t.Fatalf("expected stored application association to %s, got %+v", appB.Id, stored.Applications)
	}
}

func TestUpdateNotificationHandler_NotFound(t *testing.T) {
	setupTestDBWithNotifications(t)

	reqBody, _ := json.Marshal(map[string]any{
		"name":   "Updated Alerts",
		"status": "healthy",
		"type":   "discord",
		"config": "discord://token@channel",
	})

	router := gin.New()
	router.PUT("/api/v1/notifications/:id", UpdateNotificationHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/notifications/missing", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteNotificationHandler_Success(t *testing.T) {
	setupTestDBWithNotifications(t)

	notification := createNotificationRecord(t, nil)

	router := gin.New()
	router.DELETE("/api/v1/notifications/:id", DeleteNotificationHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/notifications/"+notification.Id, nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	_, err := gorm.G[models.Notification](db.DB).Where("id = ?", notification.Id).First(t.Context())
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected deleted notification to be missing, got err=%v", err)
	}
}

func TestDeleteNotificationHandler_NotFound(t *testing.T) {
	setupTestDBWithNotifications(t)

	router := gin.New()
	router.DELETE("/api/v1/notifications/:id", DeleteNotificationHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/notifications/missing", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTestNotificationHandler_Success(t *testing.T) {
	setupTestDBWithNotifications(t)

	notification := createNotificationRecord(t, nil)

	originalSend := sendTestNotification
	t.Cleanup(func() {
		sendTestNotification = originalSend
	})

	called := false
	sendTestNotification = func(notificationType models.NotificationType, rawConfig, message string) error {
		called = true
		if notificationType != models.NotificationTypeDiscord {
			t.Fatalf("expected discord notification type, got %q", notificationType)
		}
		if rawConfig == "" {
			t.Fatal("expected non-empty notification config")
		}
		if message != "ping" {
			t.Fatalf("expected message %q, got %q", "ping", message)
		}

		return nil
	}

	reqBody, _ := json.Marshal(map[string]any{"message": "ping"})

	router := gin.New()
	router.POST("/api/v1/notifications/:id/test", TestNotificationHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/"+notification.Id+"/test", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !called {
		t.Fatal("expected test notification sender to be called")
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["message"] != "test notification sent" {
		t.Fatalf("expected success message, got %q", body["message"])
	}
}

func TestTestNotificationHandler_NotFound(t *testing.T) {
	setupTestDBWithNotifications(t)

	originalSend := sendTestNotification
	t.Cleanup(func() {
		sendTestNotification = originalSend
	})
	sendTestNotification = func(models.NotificationType, string, string) error {
		t.Fatal("did not expect sender to be called")
		return nil
	}

	router := gin.New()
	router.POST("/api/v1/notifications/:id/test", TestNotificationHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/missing/test", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTestNotificationHandler_InvalidRequest(t *testing.T) {
	setupTestDBWithNotifications(t)

	notification := createNotificationRecord(t, nil)

	originalSend := sendTestNotification
	t.Cleanup(func() {
		sendTestNotification = originalSend
	})
	sendTestNotification = func(models.NotificationType, string, string) error {
		t.Fatal("did not expect sender to be called")
		return nil
	}

	router := gin.New()
	router.POST("/api/v1/notifications/:id/test", TestNotificationHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/"+notification.Id+"/test", strings.NewReader(`{"message":123}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTestNotificationHandler_DispatchFailure(t *testing.T) {
	setupTestDBWithNotifications(t)

	notification := createNotificationRecord(t, nil)

	originalSend := sendTestNotification
	t.Cleanup(func() {
		sendTestNotification = originalSend
	})
	sendTestNotification = func(models.NotificationType, string, string) error {
		return hubnotifications.ErrNotificationDispatch
	}

	router := gin.New()
	router.POST("/api/v1/notifications/:id/test", TestNotificationHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/"+notification.Id+"/test", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", w.Code, w.Body.String())
	}
}
