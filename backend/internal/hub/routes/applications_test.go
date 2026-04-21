package routes

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/shared/httpclient"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var invalidStatus = "invalid"
var notRfc3339 = "not-rfc3339"

const (
	mockComposeFileContent = "version: \"3.9\"\nservices:\n  billing:\n    image: ghcr.io/orcacd/billing:1.0.0\n"
	mockLatestCommitHash   = "1a2b3c4d5e6f7a8b9c0d"
	mockLatestCommitMsg    = "chore: update compose file"
)

func setupTestDBWithApplications(t *testing.T) {
	t.Helper()
	setupTestDB(t)

	if err := db.DB.AutoMigrate(&models.Agent{}, &models.Repository{}, &models.Application{}); err != nil {
		t.Fatalf("failed to migrate dependencies: %v", err)
	}

	restore := mockApplicationRepositoryHTTPClient()
	t.Cleanup(restore)
}

func jsonResponseWithBody(code int, body string) *http.Response {
	return &http.Response{
		StatusCode: code,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func mockApplicationRepositoryHTTPClient() func() {
	encodedComposeFile := base64.StdEncoding.EncodeToString([]byte(mockComposeFileContent))
	originalClient := httpclient.Default

	httpclient.Default = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case strings.Contains(req.URL.Path, "/git/trees/"):
				return jsonResponseWithBody(http.StatusOK, `{"tree":[{"path":"services/billing.yml","type":"blob"},{"path":"deployments/prod.yml","type":"blob"},{"path":"deployments/release.yml","type":"blob"},{"path":"deploy.yml","type":"blob"}]}`), nil
			case strings.Contains(req.URL.Path, "/contents/"):
				return jsonResponseWithBody(http.StatusOK, `{"content":"`+encodedComposeFile+`","encoding":"base64"}`), nil
			case strings.Contains(req.URL.Path, "/commits/"):
				return jsonResponseWithBody(http.StatusOK, `{"sha":"`+mockLatestCommitHash+`","commit":{"message":"`+mockLatestCommitMsg+`"}}`), nil
			default:
				return jsonResponseWithBody(http.StatusNotFound, `{}`), nil
			}
		}),
	}

	return func() {
		httpclient.Default = originalClient
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
		Path:          "deployments/prod.yml",
		ComposeFile:   crypto.EncryptedString("version: \"3.8\"\nservices:\n  app:\n    image: ghcr.io/orcacd/app:old\n"),
	}

	if err := db.DB.Select("*").Create(&app).Error; err != nil {
		t.Fatalf("failed to seed application: %v", err)
	}

	return app
}

func validApplicationRequestBody(repoID, agentID string) map[string]any {
	return map[string]any{
		"name":         "Billing Service",
		"repositoryId": repoID,
		"agentId":      agentID,
		"branch":       "main",
		"path":         "services/billing.yml",
	}
}

func closeDBForErrorPath(t *testing.T) {
	t.Helper()

	sqlDB, err := db.DB.DB()
	if err != nil {
		t.Fatalf("failed to get sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("failed to close sql db: %v", err)
	}
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
	if item["name"] != "Summary App" {
		t.Errorf("expected name %q, got %v", "Summary App", item["name"])
	}
	if item["repositoryName"] != repo.Name {
		t.Errorf("expected repositoryName %q, got %v", repo.Name, item["repositoryName"])
	}
	if item["agentName"] != "agent-summary" {
		t.Errorf("expected agentName %q, got %v", "agent-summary", item["agentName"])
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
	if body.RepositoryName != repo.Name {
		t.Errorf("expected repositoryName %q, got %q", repo.Name, body.RepositoryName)
	}
	if body.AgentName != "agent-detail" {
		t.Errorf("expected agentName %q, got %q", "agent-detail", body.AgentName)
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
		{name: "missing name", body: map[string]any{"repositoryId": "repo", "agentId": "agent", "branch": "main", "path": "deploy"}},
		{name: "missing repositoryId", body: map[string]any{"name": "app", "agentId": "agent", "branch": "main", "path": "deploy"}},
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

	reqBody, _ := json.Marshal(map[string]any{
		"name":         "Billing Service",
		"repositoryId": repo.Id,
		"agentId":      agent.Id,
		"branch":       "main",
		"path":         "services/billing.yml",
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
	if body.RepositoryName != repo.Name {
		t.Errorf("expected repositoryName %q, got %q", repo.Name, body.RepositoryName)
	}
	if body.AgentName != "agent-create" {
		t.Errorf("expected agentName %q, got %q", "agent-create", body.AgentName)
	}
	if body.SyncStatus != string(models.UnknownSync) {
		t.Errorf("expected syncStatus %q, got %q", models.UnknownSync, body.SyncStatus)
	}
	if body.HealthStatus != string(models.UnknownHealth) {
		t.Errorf("expected healthStatus %q, got %q", models.UnknownHealth, body.HealthStatus)
	}
	if body.Commit != mockLatestCommitHash {
		t.Errorf("expected commit %q, got %q", mockLatestCommitHash, body.Commit)
	}
	if body.CommitMessage != mockLatestCommitMsg {
		t.Errorf("expected commitMessage %q, got %q", mockLatestCommitMsg, body.CommitMessage)
	}
	if body.LastSyncedAt != nil {
		t.Errorf("expected lastSyncedAt to be null, got %v", *body.LastSyncedAt)
	}

	stored, err := gorm.G[models.Application](db.DB).Where("id = ?", body.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to find application in DB: %v", err)
	}
	if stored.Name.String() != "Billing Service" {
		t.Errorf("expected encrypted/decrypted name %q, got %q", "Billing Service", stored.Name.String())
	}
	if stored.SyncStatus != models.UnknownSync {
		t.Errorf("expected syncStatus %q, got %q", models.UnknownSync, stored.SyncStatus)
	}
	if stored.HealthStatus != models.UnknownHealth {
		t.Errorf("expected healthStatus %q, got %q", models.UnknownHealth, stored.HealthStatus)
	}
	if stored.Commit != mockLatestCommitHash {
		t.Errorf("expected commit %q, got %q", mockLatestCommitHash, stored.Commit)
	}
	if stored.CommitMessage != mockLatestCommitMsg {
		t.Errorf("expected commitMessage %q, got %q", mockLatestCommitMsg, stored.CommitMessage)
	}
	if stored.ComposeFile.String() != mockComposeFileContent {
		t.Errorf("expected composeFile to match fetched content")
	}
	if stored.LastSyncedAt != nil {
		t.Fatal("expected LastSyncedAt to be nil in DB")
	}
}

func TestCreateApplicationHandler_IgnoresSyncStatusInput(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-create-invalid-sync")
	agent := seedTestAgent(t, "agent-create-invalid-sync")
	req := validApplicationRequestBody(repo.Id, agent.Id)
	req["syncStatus"] = invalidStatus

	reqBody, _ := json.Marshal(req)

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
	if body.SyncStatus != string(models.UnknownSync) {
		t.Errorf("expected syncStatus %q, got %q", models.UnknownSync, body.SyncStatus)
	}
}

func TestCreateApplicationHandler_InvalidPathExtension(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-create-invalid-path-ext")
	agent := seedTestAgent(t, "agent-create-invalid-path-ext")
	req := validApplicationRequestBody(repo.Id, agent.Id)
	req["path"] = "services/billing.txt"

	reqBody, _ := json.Marshal(req)

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/applications", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	CreateApplicationHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateApplicationHandler_PathNotFound(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-create-path-not-found")
	agent := seedTestAgent(t, "agent-create-path-not-found")
	req := validApplicationRequestBody(repo.Id, agent.Id)
	req["path"] = "services/missing.yml"

	reqBody, _ := json.Marshal(req)

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/applications", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	CreateApplicationHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateApplicationHandler_IgnoresHealthStatusInput(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-create-invalid-health")
	agent := seedTestAgent(t, "agent-create-invalid-health")
	req := validApplicationRequestBody(repo.Id, agent.Id)
	req["healthStatus"] = invalidStatus

	reqBody, _ := json.Marshal(req)

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
	if body.HealthStatus != string(models.UnknownHealth) {
		t.Errorf("expected healthStatus %q, got %q", models.UnknownHealth, body.HealthStatus)
	}
}

func TestCreateApplicationHandler_IgnoresLastSyncedAtInput(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-create-invalid-synced-at")
	agent := seedTestAgent(t, "agent-create-invalid-synced-at")
	req := validApplicationRequestBody(repo.Id, agent.Id)
	req["lastSyncedAt"] = notRfc3339

	reqBody, _ := json.Marshal(req)

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
	if body.LastSyncedAt != nil {
		t.Fatalf("expected lastSyncedAt to be null, got %v", *body.LastSyncedAt)
	}
}

func TestCreateApplicationHandler_RepositoryNotFound(t *testing.T) {
	setupTestDBWithApplications(t)

	agent := seedTestAgent(t, "agent-create-missing-repo")
	req := validApplicationRequestBody("missing-repo", agent.Id)

	reqBody, _ := json.Marshal(req)

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/applications", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	CreateApplicationHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateApplicationHandler_AgentNotFound(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-create-missing-agent")
	req := validApplicationRequestBody(repo.Id, "missing-agent")

	reqBody, _ := json.Marshal(req)

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/applications", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	CreateApplicationHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateApplicationHandler_EmptyLastSyncedAt(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-create-empty-synced-at")
	agent := seedTestAgent(t, "agent-create-empty-synced-at")
	req := validApplicationRequestBody(repo.Id, agent.Id)
	req["lastSyncedAt"] = ""

	reqBody, _ := json.Marshal(req)

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
	if body.LastSyncedAt != nil {
		t.Fatalf("expected lastSyncedAt to be null, got %v", *body.LastSyncedAt)
	}

	stored, err := gorm.G[models.Application](db.DB).Where("id = ?", body.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to find application in DB: %v", err)
	}
	if stored.LastSyncedAt != nil {
		t.Fatal("expected LastSyncedAt to be nil in DB")
	}
}

func TestCreateApplicationHandler_DBError(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-create-db-error")
	agent := seedTestAgent(t, "agent-create-db-error")
	req := validApplicationRequestBody(repo.Id, agent.Id)

	closeDBForErrorPath(t)

	reqBody, _ := json.Marshal(req)

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/applications", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	CreateApplicationHandler(c)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateApplicationHandler_Success(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-update")
	agent := seedTestAgent(t, "agent-update")
	app := seedTestApplication(t, repo.Id, agent.Id, "Old Name")

	newRepo := seedTestRepository(t, "https://github.com/owner/repo-update-2")
	newAgent := seedTestAgent(t, "agent-update-2")

	reqBody, _ := json.Marshal(map[string]any{
		"name":         "New Name",
		"repositoryId": newRepo.Id,
		"agentId":      newAgent.Id,
		"branch":       "release",
		"path":         "deployments/release.yml",
	})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/applications/"+app.Id, bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: app.Id}}

	UpdateApplicationHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body applicationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.RepositoryName != newRepo.Name {
		t.Errorf("expected repositoryName %q, got %q", newRepo.Name, body.RepositoryName)
	}
	if body.AgentName != "agent-update-2" {
		t.Errorf("expected agentName %q, got %q", "agent-update-2", body.AgentName)
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
	if updated.Branch != "release" {
		t.Errorf("expected branch %q, got %q", "release", updated.Branch)
	}
	if updated.Path != "deployments/release.yml" {
		t.Errorf("expected path %q, got %q", "deployments/release.yml", updated.Path)
	}
	if updated.SyncStatus != models.UnknownSync {
		t.Errorf("expected syncStatus %q, got %q", models.UnknownSync, updated.SyncStatus)
	}
	if updated.HealthStatus != models.UnknownHealth {
		t.Errorf("expected healthStatus %q, got %q", models.UnknownHealth, updated.HealthStatus)
	}
	if updated.Commit != mockLatestCommitHash {
		t.Errorf("expected commit %q, got %q", mockLatestCommitHash, updated.Commit)
	}
	if updated.CommitMessage != mockLatestCommitMsg {
		t.Errorf("expected commitMessage %q, got %q", mockLatestCommitMsg, updated.CommitMessage)
	}
	if updated.ComposeFile.String() != mockComposeFileContent {
		t.Errorf("expected composeFile to match fetched content")
	}
}

func TestUpdateApplicationHandler_IgnoresSyncStatusInput(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-invalid-sync")
	agent := seedTestAgent(t, "agent-invalid-sync")
	app := seedTestApplication(t, repo.Id, agent.Id, "Sync App")

	reqBody, _ := json.Marshal(map[string]any{
		"name":          "Sync App",
		"repositoryId":  repo.Id,
		"agentId":       agent.Id,
		"syncStatus":    invalidStatus,
		"healthStatus":  "unknown",
		"branch":        "main",
		"commit":        "abc",
		"commitMessage": "msg",
		"path":          "deploy.yml",
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
	if updated.SyncStatus != models.UnknownSync {
		t.Errorf("expected syncStatus %q, got %q", models.UnknownSync, updated.SyncStatus)
	}
}

func TestUpdateApplicationHandler_InvalidPathExtension(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-update-invalid-path-ext")
	agent := seedTestAgent(t, "agent-update-invalid-path-ext")
	app := seedTestApplication(t, repo.Id, agent.Id, "Path App")
	req := validApplicationRequestBody(repo.Id, agent.Id)
	req["path"] = "services/billing.txt"

	reqBody, _ := json.Marshal(req)

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/applications/"+app.Id, bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: app.Id}}

	UpdateApplicationHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateApplicationHandler_InvalidRequest(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-update-invalid-request")
	agent := seedTestAgent(t, "agent-update-invalid-request")
	app := seedTestApplication(t, repo.Id, agent.Id, "Request App")

	reqBody, _ := json.Marshal(map[string]any{
		"repositoryId": repo.Id,
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

func TestUpdateApplicationHandler_IgnoresHealthStatusInput(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-update-invalid-health")
	agent := seedTestAgent(t, "agent-update-invalid-health")
	app := seedTestApplication(t, repo.Id, agent.Id, "Health App")
	req := validApplicationRequestBody(repo.Id, agent.Id)
	req["healthStatus"] = invalidStatus

	reqBody, _ := json.Marshal(req)

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
	if updated.HealthStatus != models.UnknownHealth {
		t.Errorf("expected healthStatus %q, got %q", models.UnknownHealth, updated.HealthStatus)
	}
}

func TestUpdateApplicationHandler_IgnoresLastSyncedAtInput(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-update-invalid-synced-at")
	agent := seedTestAgent(t, "agent-update-invalid-synced-at")
	app := seedTestApplication(t, repo.Id, agent.Id, "Synced App")
	req := validApplicationRequestBody(repo.Id, agent.Id)
	req["lastSyncedAt"] = notRfc3339

	reqBody, _ := json.Marshal(req)

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
	if updated.LastSyncedAt == nil {
		t.Fatal("expected LastSyncedAt to remain unchanged")
	}
}

func TestUpdateApplicationHandler_NotFound(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-update-not-found")
	agent := seedTestAgent(t, "agent-update-not-found")
	req := validApplicationRequestBody(repo.Id, agent.Id)

	reqBody, _ := json.Marshal(req)

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/applications/missing-app", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: "missing-app"}}

	UpdateApplicationHandler(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateApplicationHandler_RepositoryNotFound(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-update-missing-repo")
	agent := seedTestAgent(t, "agent-update-missing-repo")
	app := seedTestApplication(t, repo.Id, agent.Id, "Repo App")
	req := validApplicationRequestBody("missing-repo", agent.Id)

	reqBody, _ := json.Marshal(req)

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/applications/"+app.Id, bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: app.Id}}

	UpdateApplicationHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateApplicationHandler_AgentNotFound(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-update-missing-agent")
	agent := seedTestAgent(t, "agent-update-missing-agent")
	app := seedTestApplication(t, repo.Id, agent.Id, "Agent App")
	req := validApplicationRequestBody(repo.Id, "missing-agent")

	reqBody, _ := json.Marshal(req)

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/applications/"+app.Id, bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: app.Id}}

	UpdateApplicationHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateApplicationHandler_DBError(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-update-db-error")
	agent := seedTestAgent(t, "agent-update-db-error")
	app := seedTestApplication(t, repo.Id, agent.Id, "DB Error App")
	req := validApplicationRequestBody(repo.Id, agent.Id)

	closeDBForErrorPath(t)

	reqBody, _ := json.Marshal(req)

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/applications/"+app.Id, bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: app.Id}}

	UpdateApplicationHandler(c)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
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

func TestListApplicationsHandler_DBError(t *testing.T) {
	setupTestDBWithApplications(t)

	closeDBForErrorPath(t)

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/applications", nil)

	ListApplicationsHandler(c)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetApplicationHandler_DBError(t *testing.T) {
	setupTestDBWithApplications(t)

	closeDBForErrorPath(t)

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/applications/any-id", nil)
	c.Params = gin.Params{{Key: "id", Value: "any-id"}}

	GetApplicationHandler(c)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteApplicationHandler_DBError(t *testing.T) {
	setupTestDBWithApplications(t)

	closeDBForErrorPath(t)

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/applications/any-id", nil)
	c.Params = gin.Params{{Key: "id", Value: "any-id"}}

	DeleteApplicationHandler(c)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestParseRFC3339Timestamp(t *testing.T) {
	t.Run("nil value", func(t *testing.T) {
		parsed, ok := parseRFC3339Timestamp(nil)
		if !ok {
			t.Fatal("expected nil value to be accepted")
		}
		if parsed != nil {
			t.Fatal("expected parsed time to be nil")
		}
	})

	t.Run("empty value", func(t *testing.T) {
		empty := ""
		parsed, ok := parseRFC3339Timestamp(&empty)
		if !ok {
			t.Fatal("expected empty value to be accepted")
		}
		if parsed != nil {
			t.Fatal("expected parsed time to be nil")
		}
	})

	t.Run("invalid value", func(t *testing.T) {
		invalid := notRfc3339
		parsed, ok := parseRFC3339Timestamp(&invalid)
		if ok {
			t.Fatal("expected invalid value to be rejected")
		}
		if parsed != nil {
			t.Fatal("expected parsed time to be nil on invalid input")
		}
	})
}

func TestFormatTimestamp_Nil(t *testing.T) {
	if value := formatTimestamp(nil); value != nil {
		t.Fatalf("expected nil, got %v", *value)
	}
}
