package routes

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	application_deployer "github.com/OrcaCD/orca-cd/internal/hub/deployer"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/websocket"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/shared/httpclient"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

var invalidStatus = "invalid"
var notRfc3339 = "not-rfc3339"

const (
	mockComposeFileContent = "version: \"3.9\"\nservices:\n  billing:\n    image: ghcr.io/orcacd/billing:1.0.0\n"
	mockLatestCommitHash   = "1a2b3c4d5e6f7a8b9c0d"
	mockLatestCommitMsg    = "chore: update compose file"
	seedComposeFileContent = "version: \"3.8\"\nservices:\n  app:\n    image: ghcr.io/orcacd/app:old\n"
)

type stubRouteDeployer struct {
	startErr    error
	startedApp  string
	startedWith string
	requestID   string
	rolledBack  *models.ApplicationDeployment
}

func (d *stubRouteDeployer) TriggerRollback(_ context.Context, app *models.Application, target *models.ApplicationDeployment, requestID string) error {
	d.startedApp = app.Id
	d.rolledBack = target
	d.requestID = requestID
	return d.startErr
}

func (d *stubRouteDeployer) TriggerApplicationDeploy(ctx context.Context, app *models.Application, composeFile, requestID string) error {
	d.startedApp = app.Id
	d.startedWith = composeFile
	d.requestID = requestID
	if d.startErr != nil {
		return d.startErr
	}
	return nil
}

func setupTestDBWithApplications(t *testing.T) {
	t.Helper()
	setupTestDB(t)

	if err := db.DB.AutoMigrate(&models.Agent{}, &models.Repository{}, &models.Application{}, &models.ApplicationEvent{}, &models.ApplicationDeployment{}); err != nil {
		t.Fatalf("failed to migrate dependencies: %v", err)
	}

	// AutoMigrate builds the schema from model tags, which do not include the
	// per-agent name_hash uniqueness index (it lives in the SQL migrations, not a
	// GORM tag). Recreate it here so handlers exercise the real DB-level
	// enforcement — mirrors migration 000023.
	if err := db.DB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_applications_agent_name_hash ON applications (agent_id, name_hash) WHERE name_hash != ''`).Error; err != nil {
		t.Fatalf("failed to create name_hash unique index: %v", err)
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
		Icon:   "server",
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
		Name:                crypto.EncryptedString(name),
		NameHash:            crypto.BlindIndex(models.NormalizeName(name)),
		Icon:                "box",
		RepositoryId:        repoId,
		AgentId:             agentId,
		SyncStatus:          models.UnknownSync,
		HealthStatus:        models.UnknownHealth,
		Branch:              "main",
		Commit:              "abcdef123",
		CommitMessage:       "initial commit",
		LastSyncedAt:        &now,
		Path:                "deployments/prod.yml",
		ComposeFile:         crypto.EncryptedString(seedComposeFileContent),
		PreviousComposeFile: crypto.EncryptedString(""),
	}

	if err := db.DB.Select("*").Create(&app).Error; err != nil {
		t.Fatalf("failed to seed application: %v", err)
	}

	return app
}

func validApplicationRequestBody(repoID, agentID string) map[string]any {
	return map[string]any{
		"name":         "Billing Service",
		"icon":         "box",
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
	if item["icon"] != "box" {
		t.Errorf("expected icon %q, got %v", "box", item["icon"])
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
	if item["repositoryId"] != repo.Id {
		t.Errorf("expected repositoryId %q, got %v", repo.Id, item["repositoryId"])
	}
	if item["repositoryName"] != repo.Name {
		t.Errorf("expected repositoryName %q, got %v", repo.Name, item["repositoryName"])
	}
	if item["agentName"] != "agent-summary" {
		t.Errorf("expected agentName %q, got %v", "agent-summary", item["agentName"])
	}
	if item["agentId"] != agent.Id {
		t.Errorf("expected agentId %q, got %v", agent.Id, item["agentId"])
	}
	if _, ok := item["lastSyncError"]; ok {
		t.Errorf("expected lastSyncError to be omitted, got %v", item["lastSyncError"])
	}
}

func TestListApplicationsHandler_IncludesLastSyncError(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-sync-error")
	agent := seedTestAgent(t, "agent-sync-error")
	app := seedTestApplication(t, repo.Id, agent.Id, "Failing App")
	if _, err := gorm.G[models.Application](db.DB).
		Where("id = ?", app.Id).
		Update(t.Context(), "last_sync_error", "pull access denied"); err != nil {
		t.Fatalf("set last sync error: %v", err)
	}

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/applications", nil)

	ListApplicationsHandler(c)

	var body []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(body) != 1 || body[0]["lastSyncError"] != "pull access denied" {
		t.Fatalf("expected lastSyncError in list response, got %v", body)
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
	if body.RepositorySyncType != string(repo.SyncType) {
		t.Errorf("expected repositorySyncType %q, got %q", repo.SyncType, body.RepositorySyncType)
	}
	if body.AgentName != "agent-detail" {
		t.Errorf("expected agentName %q, got %q", "agent-detail", body.AgentName)
	}
	if body.CommitMessage != "initial commit" {
		t.Errorf("expected commitMessage %q, got %q", "initial commit", body.CommitMessage)
	}
	if body.PreviousComposeFile != "" {
		t.Errorf("expected previousComposeFile to be empty, got %q", body.PreviousComposeFile)
	}
	if body.CreatedAt == "" {
		t.Error("expected createdAt to be set")
	}
	if body.UpdatedAt == "" {
		t.Error("expected updatedAt to be set")
	}
	if body.LastSyncSource != nil {
		t.Errorf("expected lastSyncSource to be omitted, got %q", *body.LastSyncSource)
	}
}

func TestGetApplicationHandler_IncludesLastSyncSource(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-sync-source")
	agent := seedTestAgent(t, "agent-sync-source")
	app := seedTestApplication(t, repo.Id, agent.Id, "Sync Source")
	source := models.ApplicationEventSourceRepositoryWebhook
	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).Updates(t.Context(), models.Application{LastSyncSource: &source}); err != nil {
		t.Fatalf("failed to set last sync source: %v", err)
	}

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
	if body.LastSyncSource == nil || *body.LastSyncSource != string(source) {
		t.Errorf("expected lastSyncSource %q, got %v", source, body.LastSyncSource)
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
	deployer := &stubRouteDeployer{}
	application_deployer.DefaultApplicationDeployer = deployer
	t.Cleanup(func() { application_deployer.DefaultApplicationDeployer = nil })

	repo := seedTestRepository(t, "https://github.com/owner/repo-create")
	agent := seedTestAgent(t, "agent-create")

	reqBody, _ := json.Marshal(map[string]any{
		"name":         "Billing Service",
		"icon":         "badge-dollar-sign",
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
	if body.Icon != "badge-dollar-sign" {
		t.Errorf("expected icon %q, got %q", "badge-dollar-sign", body.Icon)
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
	if body.PreviousComposeFile != "" {
		t.Errorf("expected previousComposeFile to be empty, got %q", body.PreviousComposeFile)
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
	if stored.Icon != "badge-dollar-sign" {
		t.Errorf("expected stored icon %q, got %q", "badge-dollar-sign", stored.Icon)
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
	if stored.PreviousComposeFile.String() != "" {
		t.Errorf("expected previous compose file to be empty on create")
	}
	if stored.LastSyncedAt != nil {
		t.Fatal("expected LastSyncedAt to be nil in DB")
	}
	event, err := gorm.G[models.ApplicationEvent](db.DB).Where("request_id = ?", deployer.requestID).First(t.Context())
	if err != nil {
		t.Fatalf("load initial deployment event: %v", err)
	}
	if event.Type != models.ApplicationEventDeployment ||
		event.Source != models.ApplicationEventSourceApplicationCreated ||
		event.ActorName == nil || *event.ActorName != "Test User" {
		t.Fatalf("unexpected initial deployment event: %+v", event)
	}
}

func TestCreateApplicationHandler_DuplicateName(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-dup")
	agent := seedTestAgent(t, "agent-dup")
	seedTestApplication(t, repo.Id, agent.Id, "Billing Service")

	// Different case must still collide (case-insensitive check).
	reqBody, _ := json.Marshal(map[string]any{
		"name":         "billing service",
		"repositoryId": repo.Id,
		"agentId":      agent.Id,
		"branch":       "main",
		"path":         "services/billing.yml",
	})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/applications", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	CreateApplicationHandler(c)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}

	var count int64
	db.DB.Model(&models.Application{}).Count(&count)
	if count != 1 {
		t.Errorf("expected no second application created, got %d", count)
	}
}

func TestCreateApplicationHandler_SameNameDifferentAgentAllowed(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-multiagent")
	agentA := seedTestAgent(t, "agent-a")
	agentB := seedTestAgent(t, "agent-b")
	seedTestApplication(t, repo.Id, agentA.Id, "Billing Service")

	// Same name, different agent — uniqueness is scoped per agent, so this is allowed.
	reqBody, _ := json.Marshal(map[string]any{
		"name":         "Billing Service",
		"repositoryId": repo.Id,
		"agentId":      agentB.Id,
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

	var count int64
	db.DB.Model(&models.Application{}).Count(&count)
	if count != 2 {
		t.Errorf("expected two applications, got %d", count)
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
	deletes := withDeletingAgent(t, agent.Id, true, "")

	reqBody, _ := json.Marshal(map[string]any{
		"name":         "New Name",
		"icon":         "rocket",
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
	if body.Icon != "rocket" {
		t.Errorf("expected icon %q, got %q", "rocket", body.Icon)
	}

	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load updated application: %v", err)
	}

	if updated.Name.String() != "New Name" {
		t.Errorf("expected name %q, got %q", "New Name", updated.Name.String())
	}
	if updated.Icon != "rocket" {
		t.Errorf("expected icon %q, got %q", "rocket", updated.Icon)
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
	if updated.PreviousComposeFile.String() != seedComposeFileContent {
		t.Errorf("expected previous compose file to be set to the pre-update content")
	}
	if got := deletes.Load(); got != 1 {
		t.Errorf("expected old agent to receive 1 delete request, got %d", got)
	}
}

func updateApplicationAgent(t *testing.T, app models.Application, agentID, name string) *httptest.ResponseRecorder {
	t.Helper()
	reqBody, _ := json.Marshal(map[string]any{
		"name":         name,
		"repositoryId": app.RepositoryId,
		"agentId":      agentID,
		"branch":       "main",
		"path":         "docker-compose.yml",
	})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/applications/"+app.Id, bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: app.Id}}

	UpdateApplicationHandler(c)
	return w
}

func assertApplicationAgent(t *testing.T, appID, agentID string) {
	t.Helper()
	app, err := gorm.G[models.Application](db.DB).Where("id = ?", appID).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load application: %v", err)
	}
	if app.AgentId != agentID {
		t.Errorf("expected agentId %q, got %q", agentID, app.AgentId)
	}
}

func TestUpdateApplicationHandler_SameComposeProject_DoesNotUndeploy(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-update-same-agent")
	agent := seedTestAgent(t, "agent-update-same")
	app := seedTestApplication(t, repo.Id, agent.Id, "Same Agent")
	deletes := withDeletingAgent(t, agent.Id, true, "")

	// "same agent" normalizes to the same compose project as "Same Agent".
	w := updateApplicationAgent(t, app, agent.Id, "same agent")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := deletes.Load(); got != 0 {
		t.Errorf("expected no delete request, got %d", got)
	}
}

func TestUpdateApplicationHandler_Rename_UndeploysOldProject(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-rename")
	agent := seedTestAgent(t, "agent-rename")
	app := seedTestApplication(t, repo.Id, agent.Id, "Old Name")
	deletes := withDeletingAgent(t, agent.Id, true, "")

	w := updateApplicationAgent(t, app, agent.Id, "New Name")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := deletes.Load(); got != 1 {
		t.Errorf("expected 1 delete request, got %d", got)
	}
}

func TestUpdateApplicationHandler_Rename_AgentOffline_KeepsName(t *testing.T) {
	setupTestDBWithApplications(t)
	withOfflineHub(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-rename-offline")
	agent := seedTestAgent(t, "agent-rename-offline")
	app := seedTestApplication(t, repo.Id, agent.Id, "Old Name")

	w := updateApplicationAgent(t, app, agent.Id, "New Name")

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load application: %v", err)
	}
	if updated.Name.String() != "Old Name" {
		t.Errorf("expected name %q, got %q", "Old Name", updated.Name.String())
	}
}

func TestComposeProjectChanged(t *testing.T) {
	tests := []struct {
		oldName, newName string
		want             bool
	}{
		{"Billing", "billing", false},
		{"My App", "my-app", false},
		{"Billing", "Payments", true},
		{"---", "Payments", false},
	}
	for _, tt := range tests {
		if got := composeProjectChanged(tt.oldName, tt.newName); got != tt.want {
			t.Errorf("composeProjectChanged(%q, %q) = %v, want %v", tt.oldName, tt.newName, got, tt.want)
		}
	}
}

func TestUpdateApplicationHandler_ChangeAgent_OldAgentOffline_KeepsApp(t *testing.T) {
	setupTestDBWithApplications(t)
	withOfflineHub(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-move-offline")
	oldAgent := seedTestAgent(t, "agent-move-offline-old")
	newAgent := seedTestAgent(t, "agent-move-offline-new")
	app := seedTestApplication(t, repo.Id, oldAgent.Id, "Move Offline")

	w := updateApplicationAgent(t, app, newAgent.Id, "Move Offline")

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	assertApplicationAgent(t, app.Id, oldAgent.Id)
}

func TestUpdateApplicationHandler_ChangeAgent_OldAgentFailure_KeepsApp(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-move-fail")
	oldAgent := seedTestAgent(t, "agent-move-fail-old")
	newAgent := seedTestAgent(t, "agent-move-fail-new")
	app := seedTestApplication(t, repo.Id, oldAgent.Id, "Move Fail")
	withDeletingAgent(t, oldAgent.Id, false, "compose down: boom")

	w := updateApplicationAgent(t, app, newAgent.Id, "Move Fail")

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", w.Code, w.Body.String())
	}
	assertApplicationAgent(t, app.Id, oldAgent.Id)
}

func TestUpdateApplicationHandler_ChangeAgent_NameConflict_DoesNotUndeploy(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-move-conflict")
	oldAgent := seedTestAgent(t, "agent-move-conflict-old")
	newAgent := seedTestAgent(t, "agent-move-conflict-new")
	app := seedTestApplication(t, repo.Id, oldAgent.Id, "Billing Service")
	seedTestApplication(t, repo.Id, newAgent.Id, "billing service")
	deletes := withDeletingAgent(t, oldAgent.Id, true, "")

	w := updateApplicationAgent(t, app, newAgent.Id, "Billing Service")

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if got := deletes.Load(); got != 0 {
		t.Errorf("expected no delete request, got %d", got)
	}
	assertApplicationAgent(t, app.Id, oldAgent.Id)
}

func TestUpdateApplicationHandler_SavesDisabledFlagsAndKeepsRuntimeColumns(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-update-flags")
	agent := seedTestAgent(t, "agent-update-flags")
	app := seedTestApplication(t, repo.Id, agent.Id, "Flags")

	secret := crypto.EncryptedString("webhook-secret")
	syncError := "previous failure"
	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).Updates(t.Context(), models.Application{
		ImagePollEnabled:   true,
		DeleteOldImages:    true,
		ImageWebhookSecret: &secret,
		LastSyncError:      &syncError,
		SyncStatus:         models.OutOfSync,
		HealthStatus:       models.Healthy,
	}); err != nil {
		t.Fatalf("failed to prepare application: %v", err)
	}

	reqBody, _ := json.Marshal(map[string]any{
		"name":             "Flags",
		"repositoryId":     repo.Id,
		"agentId":          agent.Id,
		"branch":           "main",
		"path":             "deployments/prod.yml",
		"imagePollEnabled": false,
		"deleteOldImages":  false,
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
	if updated.ImagePollEnabled || updated.DeleteOldImages {
		t.Errorf("expected disabled flags to be saved, got imagePollEnabled=%v deleteOldImages=%v", updated.ImagePollEnabled, updated.DeleteOldImages)
	}
	if updated.ImageWebhookSecret == nil || updated.ImageWebhookSecret.String() != "webhook-secret" {
		t.Error("expected the image webhook secret to be kept")
	}
	if updated.LastSyncError == nil || *updated.LastSyncError != syncError {
		t.Errorf("expected lastSyncError %q to be kept", syncError)
	}
	if updated.SyncStatus != models.OutOfSync || updated.HealthStatus != models.Healthy {
		t.Errorf("expected sync and health status to be kept, got %q / %q", updated.SyncStatus, updated.HealthStatus)
	}
}

func TestApplicationHandlers_RejectInvalidImagePollInterval(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-poll-interval")
	agent := seedTestAgent(t, "agent-poll-interval")
	app := seedTestApplication(t, repo.Id, agent.Id, "Poll Interval")

	tests := []struct {
		name     string
		enabled  bool
		interval int64
	}{
		{name: "enabled below the minimum", enabled: true, interval: 59},
		{name: "enabled with zero", enabled: true, interval: 0},
		{name: "enabled negative", enabled: true, interval: -60},
		{name: "disabled negative", enabled: false, interval: -1},
	}

	for _, tt := range tests {
		body, _ := json.Marshal(map[string]any{
			"name":                     "Poll Interval",
			"repositoryId":             repo.Id,
			"agentId":                  agent.Id,
			"branch":                   "main",
			"path":                     "deployments/prod.yml",
			"imagePollEnabled":         tt.enabled,
			"imagePollIntervalSeconds": tt.interval,
		})

		t.Run("create "+tt.name, func(t *testing.T) {
			c, w := makeAuthContext(t, "user-1")
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/applications", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")

			CreateApplicationHandler(c)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})

		t.Run("update "+tt.name, func(t *testing.T) {
			c, w := makeAuthContext(t, "user-1")
			c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/applications/"+app.Id, bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Params = gin.Params{{Key: "id", Value: app.Id}}

			UpdateApplicationHandler(c)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
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
	app := seedTestApplication(t, repo.Id, agent.Id, "Billing Service")
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
	app := seedTestApplication(t, repo.Id, agent.Id, "Billing Service")
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

// withDeletingAgent installs a hub with a fake connected agent that auto-replies to
// the DeleteRequest with the given outcome, so DeleteApplicationHandler can run its
// confirm-before-delete flow without a real agent. Returns the received request count.
func withDeletingAgent(t *testing.T, agentID string, success bool, errMsg string) *atomic.Int32 {
	t.Helper()
	received := &atomic.Int32{}
	nop := zerolog.Nop()
	hub := websocket.NewHub(&nop)
	websocket.DefaultHub = hub
	t.Cleanup(func() { websocket.DefaultHub = nil })

	client, err := hub.Register(agentID, nil)
	if err != nil {
		t.Fatalf("failed to register fake agent: %v", err)
	}
	go func() {
		for msg := range client.Send {
			req := msg.GetDeleteRequest()
			if req == nil {
				continue
			}
			received.Add(1)
			hub.ResolveDeleteResult(agentID, &messages.DeleteResult{
				RequestId:     req.RequestId,
				ApplicationId: req.ApplicationId,
				Success:       success,
				ErrorMessage:  errMsg,
			})
		}
	}()
	return received
}

// withOfflineHub installs a hub with no connected agents.
func withOfflineHub(t *testing.T) {
	t.Helper()
	nop := zerolog.Nop()
	websocket.DefaultHub = websocket.NewHub(&nop)
	t.Cleanup(func() { websocket.DefaultHub = nil })
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
	withDeletingAgent(t, agent.Id, true, "")

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

func TestDeleteApplicationHandler_AgentOffline_KeepsApp(t *testing.T) {
	setupTestDBWithApplications(t)
	withOfflineHub(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-del-offline")
	agent := seedTestAgent(t, "agent-del-offline")
	app := seedTestApplication(t, repo.Id, agent.Id, "Offline Delete")

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/applications/"+app.Id, nil)
	c.Params = gin.Params{{Key: "id", Value: app.Id}}

	DeleteApplicationHandler(c)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	var count int64
	db.DB.Model(&models.Application{}).Where("id = ?", app.Id).Count(&count)
	if count != 1 {
		t.Error("expected application to remain when agent is offline")
	}
}

func TestDeleteApplicationHandler_AgentFailure_KeepsApp(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-del-fail")
	agent := seedTestAgent(t, "agent-del-fail")
	app := seedTestApplication(t, repo.Id, agent.Id, "Fail Delete")
	withDeletingAgent(t, agent.Id, false, "compose down: boom")

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/applications/"+app.Id, nil)
	c.Params = gin.Params{{Key: "id", Value: app.Id}}

	DeleteApplicationHandler(c)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", w.Code, w.Body.String())
	}
	var count int64
	db.DB.Model(&models.Application{}).Where("id = ?", app.Id).Count(&count)
	if count != 1 {
		t.Error("expected application to remain when agent removal fails")
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

func TestDeployApplicationHandler_Success(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-deploy")
	agent := seedTestAgent(t, "agent-deploy")
	app := seedTestApplication(t, repo.Id, agent.Id, "Deploy Me")

	deployer := &stubRouteDeployer{}
	application_deployer.DefaultApplicationDeployer = deployer
	t.Cleanup(func() { application_deployer.DefaultApplicationDeployer = nil })

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/applications/"+app.Id+"/deploy", nil)
	c.Params = gin.Params{{Key: "id", Value: app.Id}}

	DeployApplicationHandler(c)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	if deployer.startedApp != app.Id {
		t.Fatalf("expected deployer to start app %q, got %q", app.Id, deployer.startedApp)
	}
	if deployer.startedWith != seedComposeFileContent {
		t.Fatalf("expected deployer compose file to match stored compose")
	}
	if deployer.requestID == "" {
		t.Fatal("expected deployer request id")
	}
	event, err := gorm.G[models.ApplicationEvent](db.DB).Where("request_id = ?", deployer.requestID).First(t.Context())
	if err != nil {
		t.Fatalf("load deployment event: %v", err)
	}
	if event.Type != models.ApplicationEventDeployment ||
		event.Source != models.ApplicationEventSourceManual ||
		event.Status != models.ApplicationEventRunning ||
		event.ActorName == nil || *event.ActorName != "Test User" {
		t.Fatalf("unexpected manual deployment event: %+v", event)
	}
}

func TestDeployApplicationHandler_AgentUnavailable(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-deploy-offline")
	agent := seedTestAgent(t, "agent-deploy-offline")
	app := seedTestApplication(t, repo.Id, agent.Id, "Deploy Me")

	application_deployer.DefaultApplicationDeployer = &stubRouteDeployer{startErr: application_deployer.ErrAgentOffline}
	t.Cleanup(func() { application_deployer.DefaultApplicationDeployer = nil })

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/applications/"+app.Id+"/deploy", nil)
	c.Params = gin.Params{{Key: "id", Value: app.Id}}

	DeployApplicationHandler(c)

	// A deploy against an offline agent is the same conflict condition as delete:
	// the handler surfaces 409, not 500.
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPullApplicationImagesHandler_Success(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-pull")
	agent := seedTestAgent(t, "agent-pull")
	app := seedTestApplication(t, repo.Id, agent.Id, "Pull Me")

	nop := zerolog.Nop()
	hub := websocket.NewHub(&nop)
	websocket.DefaultHub = hub
	t.Cleanup(func() { websocket.DefaultHub = nil })
	client, err := hub.Register(agent.Id, nil)
	if err != nil {
		t.Fatalf("failed to register agent: %v", err)
	}

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/applications/"+app.Id+"/pull-images", nil)
	c.Params = gin.Params{{Key: "id", Value: app.Id}}

	PullApplicationImagesHandler(c)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}

	var req *messages.PullImagesRequest
	select {
	case msg := <-client.Send:
		req = msg.GetPullImagesRequest()
	default:
	}
	if req == nil || req.ApplicationId != app.Id {
		t.Fatalf("expected PullImagesRequest for app %q, got %+v", app.Id, req)
	}

	event, err := gorm.G[models.ApplicationEvent](db.DB).Where("request_id = ?", req.RequestId).First(t.Context())
	if err != nil {
		t.Fatalf("load image update event: %v", err)
	}
	if event.Type != models.ApplicationEventImageUpdate ||
		event.Source != models.ApplicationEventSourceManual ||
		event.Status != models.ApplicationEventRunning ||
		event.ActorName == nil || *event.ActorName != "Test User" {
		t.Fatalf("unexpected manual image update event: %+v", event)
	}
}

func TestPullApplicationImagesHandler_NotFound(t *testing.T) {
	setupTestDBWithApplications(t)

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/applications/nonexistent-id/pull-images", nil)
	c.Params = gin.Params{{Key: "id", Value: "nonexistent-id"}}

	PullApplicationImagesHandler(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPullApplicationImagesHandler_AgentUnavailable(t *testing.T) {
	setupTestDBWithApplications(t)
	withOfflineHub(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-pull-offline")
	agent := seedTestAgent(t, "agent-pull-offline")
	app := seedTestApplication(t, repo.Id, agent.Id, "Pull Me")

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/applications/"+app.Id+"/pull-images", nil)
	c.Params = gin.Params{{Key: "id", Value: app.Id}}

	PullApplicationImagesHandler(c)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
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

func TestGetApplicationHandler_WebhookEnabled_IncludesUrl(t *testing.T) {
	setupTestDBWithApplications(t)

	repo := seedTestRepository(t, "https://github.com/owner/repo-webhook-url")
	agent := seedTestAgent(t, "agent-webhook-url")
	app := seedTestApplication(t, repo.Id, agent.Id, "Webhook App")

	enc := crypto.EncryptedString("webhooksecret")
	if err := db.DB.Model(&models.Application{}).Where("id = ?", app.Id).Update("image_webhook_secret", &enc).Error; err != nil {
		t.Fatalf("failed to set webhook secret: %v", err)
	}

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
	if !body.ImageWebhookEnabled {
		t.Error("expected imageWebhookEnabled to be true")
	}
	if body.ImageWebhookUrl == nil || *body.ImageWebhookUrl == "" {
		t.Error("expected imageWebhookUrl to be set")
	}
}

func TestSendAgentSettings_WithHub(t *testing.T) {
	setupTestDBWithApplications(t)

	log := zerolog.Nop()
	websocket.DefaultHub = websocket.NewHub(&log)
	t.Cleanup(func() { websocket.DefaultHub = nil })

	c, _ := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	// Agent is not connected — Send returns false, but the query+SendAgentSettings call
	// path is covered. Must not panic.
	sendAgentSettings(c, "nonexistent-agent-id")
}
