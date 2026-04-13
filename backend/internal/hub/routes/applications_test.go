package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func setupTestDBWithApplications(t *testing.T) {
	t.Helper()
	setupTestDB(t)

	if err := db.DB.AutoMigrate(&models.Agent{}, &models.Repository{}, &models.Application{}); err != nil {
		t.Fatalf("failed to migrate dependencies: %v", err)
	}
}

func seedTestAgent(t *testing.T, name string) models.Agent {
	t.Helper()

	agent := models.Agent{
		Name:   crypto.EncryptedString(name),
		KeyId:  crypto.EncryptedString("test-key-" + name),
		Status: models.AgentStatusOnline,
	}
	if err := db.DB.Select("*").Create(&agent).Error; err != nil {
		t.Fatalf("failed to seed agent: %v", err)
	}

	return agent
}

func seedTestRepository(t *testing.T, url string) models.Repository {
	t.Helper()

	repo := models.Repository{
		Name:       "owner/repo",
		Url:        url,
		Provider:   models.GitHub,
		AuthMethod: models.AuthMethodNone,
		SyncType:   models.SyncTypeManual,
		SyncStatus: models.SyncStatusUnknown,
		CreatedBy:  "user-1",
	}
	if err := db.DB.Select("*").Create(&repo).Error; err != nil {
		t.Fatalf("failed to seed repository: %v", err)
	}

	return repo
}

func seedTestApplication(t *testing.T, repoId, agentId, name string) models.Application {
	t.Helper()

	now := time.Now().UTC().Truncate(time.Second)
	app := models.Application{
		Name:          crypto.EncryptedString(name),
		RepositoryId:  repoId,
		AgentId:       agentId,
		SyncStatus:    models.UnknownSync,
		HealthStatus:  models.UnknownHealth,
		Branch:        "main",
		Commit:        "abcdef123",
		CommitMessage: "initial commit",
		LastSyncedAt:  &now,
		Path:          "deployments/prod",
	}

	if err := db.DB.Select("*").Create(&app).Error; err != nil {
		t.Fatalf("failed to seed application: %v", err)
	}

	return app
}

func TestListApplicationsHandler_Empty(t *testing.T) {
	setupTestDBWithApplications(t)

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/applications", nil)

	ListApplicationsHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body []applicationListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(body) != 0 {
		t.Errorf("expected 0 applications, got %d", len(body))
	}
}

func TestListApplicationsHandler_ReturnsSummaryFields(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-summary")
	agent := seedTestAgent(t, "agent-summary")
	app := seedTestApplication(t, repo.Id, agent.Id, "Summary App")

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/applications", nil)

	ListApplicationsHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if len(body) != 1 {
		t.Fatalf("expected 1 application, got %d", len(body))
	}

	item := body[0]
	if item["id"] != app.Id {
		t.Errorf("expected id %q, got %v", app.Id, item["id"])
	}
	if item["syncStatus"] != string(models.UnknownSync) {
		t.Errorf("expected syncStatus %q, got %v", models.UnknownSync, item["syncStatus"])
	}
	if item["healthStatus"] != string(models.UnknownHealth) {
		t.Errorf("expected healthStatus %q, got %v", models.UnknownHealth, item["healthStatus"])
	}
	if item["branch"] != "main" {
		t.Errorf("expected branch %q, got %v", "main", item["branch"])
	}
	if item["commit"] != "abcdef123" {
		t.Errorf("expected commit %q, got %v", "abcdef123", item["commit"])
	}
	if _, ok := item["name"]; ok {
		t.Error("list response should not include name field")
	}
}

func TestGetApplicationHandler_NotFound(t *testing.T) {
	setupTestDBWithApplications(t)

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/applications/nonexistent-id", nil)
	c.Params = gin.Params{{Key: "id", Value: "nonexistent-id"}}

	GetApplicationHandler(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetApplicationHandler_ReturnsAllFields(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-detail")
	agent := seedTestAgent(t, "agent-detail")
	app := seedTestApplication(t, repo.Id, agent.Id, "Payments API")

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/applications/"+app.Id, nil)
	c.Params = gin.Params{{Key: "id", Value: app.Id}}

	GetApplicationHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body applicationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if body.Id != app.Id {
		t.Errorf("expected id %q, got %q", app.Id, body.Id)
	}
	if body.Name != "Payments API" {
		t.Errorf("expected name %q, got %q", "Payments API", body.Name)
	}
	if body.RepositoryId != repo.Id {
		t.Errorf("expected repositoryId %q, got %q", repo.Id, body.RepositoryId)
	}
	if body.AgentId != agent.Id {
		t.Errorf("expected agentId %q, got %q", agent.Id, body.AgentId)
	}
	if body.CommitMessage != "initial commit" {
		t.Errorf("expected commitMessage %q, got %q", "initial commit", body.CommitMessage)
	}
	if body.CreatedAt == "" {
		t.Error("expected createdAt to be set")
	}
	if body.UpdatedAt == "" {
		t.Error("expected updatedAt to be set")
	}
}

func TestCreateApplicationHandler_InvalidRequest(t *testing.T) {
	setupTestDBWithApplications(t)

	tests := []struct {
		name string
		body any
	}{
		{name: "empty body", body: nil},
		{name: "missing name", body: map[string]any{"repositoryId": "repo", "agentId": "agent", "syncStatus": "unknown", "healthStatus": "unknown", "branch": "main", "commit": "abc", "commitMessage": "msg", "path": "deploy"}},
		{name: "missing repositoryId", body: map[string]any{"name": "app", "agentId": "agent", "syncStatus": "unknown", "healthStatus": "unknown", "branch": "main", "commit": "abc", "commitMessage": "msg", "path": "deploy"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var reqBody []byte
			if tt.body != nil {
				reqBody, _ = json.Marshal(tt.body)
			}

			c, w := makeAuthContext(t, "user-1")
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/applications", bytes.NewReader(reqBody))
			c.Request.Header.Set("Content-Type", "application/json")

			CreateApplicationHandler(c)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestCreateApplicationHandler_Success(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-create")
	agent := seedTestAgent(t, "agent-create")
	syncedAt := time.Now().UTC().Truncate(time.Second)

	reqBody, _ := json.Marshal(map[string]any{
		"name":          "Billing Service",
		"repositoryId":  repo.Id,
		"agentId":       agent.Id,
		"syncStatus":    "progressing",
		"healthStatus":  "healthy",
		"branch":        "main",
		"commit":        "abc123",
		"commitMessage": "deploy billing",
		"lastSyncedAt":  syncedAt.Format(time.RFC3339),
		"path":          "services/billing",
	})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/applications", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	CreateApplicationHandler(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var body applicationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if body.Id == "" {
		t.Error("expected non-empty id")
	}
	if body.Name != "Billing Service" {
		t.Errorf("expected name %q, got %q", "Billing Service", body.Name)
	}
	if body.SyncStatus != "progressing" {
		t.Errorf("expected syncStatus %q, got %q", "progressing", body.SyncStatus)
	}

	stored, err := gorm.G[models.Application](db.DB).Where("id = ?", body.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to find application in DB: %v", err)
	}
	if stored.Name.String() != "Billing Service" {
		t.Errorf("expected encrypted/decrypted name %q, got %q", "Billing Service", stored.Name.String())
	}
}

func TestUpdateApplicationHandler_Success(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-update")
	agent := seedTestAgent(t, "agent-update")
	app := seedTestApplication(t, repo.Id, agent.Id, "Old Name")

	newRepo := seedTestRepository(t, "https://github.com/owner/repo-update-2")
	newAgent := seedTestAgent(t, "agent-update-2")
	syncedAt := time.Now().UTC().Truncate(time.Second)

	reqBody, _ := json.Marshal(map[string]any{
		"name":          "New Name",
		"repositoryId":  newRepo.Id,
		"agentId":       newAgent.Id,
		"syncStatus":    "synced",
		"healthStatus":  "healthy",
		"branch":        "release",
		"commit":        "def456",
		"commitMessage": "deploy release",
		"lastSyncedAt":  syncedAt.Format(time.RFC3339),
		"path":          "deployments/release",
	})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/applications/"+app.Id, bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: app.Id}}

	UpdateApplicationHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load updated application: %v", err)
	}

	if updated.Name.String() != "New Name" {
		t.Errorf("expected name %q, got %q", "New Name", updated.Name.String())
	}
	if updated.RepositoryId != newRepo.Id {
		t.Errorf("expected repositoryId %q, got %q", newRepo.Id, updated.RepositoryId)
	}
	if updated.AgentId != newAgent.Id {
		t.Errorf("expected agentId %q, got %q", newAgent.Id, updated.AgentId)
	}
	if updated.SyncStatus != models.Synced {
		t.Errorf("expected syncStatus %q, got %q", models.Synced, updated.SyncStatus)
	}
}

func TestUpdateApplicationHandler_InvalidSyncStatus(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-invalid-sync")
	agent := seedTestAgent(t, "agent-invalid-sync")
	app := seedTestApplication(t, repo.Id, agent.Id, "Sync App")

	reqBody, _ := json.Marshal(map[string]any{
		"name":          "Sync App",
		"repositoryId":  repo.Id,
		"agentId":       agent.Id,
		"syncStatus":    "invalid",
		"healthStatus":  "unknown",
		"branch":        "main",
		"commit":        "abc",
		"commitMessage": "msg",
		"path":          "deploy",
	})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/applications/"+app.Id, bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: app.Id}}

	UpdateApplicationHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteApplicationHandler_NotFound(t *testing.T) {
	setupTestDBWithApplications(t)

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/applications/nonexistent-id", nil)
	c.Params = gin.Params{{Key: "id", Value: "nonexistent-id"}}

	DeleteApplicationHandler(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteApplicationHandler_Success(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-delete")
	agent := seedTestAgent(t, "agent-delete")
	app := seedTestApplication(t, repo.Id, agent.Id, "Delete Me")

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/applications/"+app.Id, nil)
	c.Params = gin.Params{{Key: "id", Value: app.Id}}

	DeleteApplicationHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var count int64
	db.DB.Model(&models.Application{}).Where("id = ?", app.Id).Count(&count)
	if count != 0 {
		t.Error("expected application to be deleted from DB")
	}
}
