package routes

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/shared/httpclient"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

func setupTestDBWithRepos(t *testing.T) {
	t.Helper()
	setupTestDB(t)
	if err := db.DB.AutoMigrate(&models.Repository{}); err != nil {
		t.Fatalf("failed to migrate Repository: %v", err)
	}
}

func makeAuthContext(t *testing.T, userID string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	claims := &auth.UserClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: userID},
		Name:             "Test User",
		Email:            "test@example.com",
		Role:             "admin",
	}
	auth.SetClaims(c, claims)
	return c, w
}

func TestListRepositoriesHandler_Empty(t *testing.T) {
	setupTestDBWithRepos(t)

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/repositories", nil)

	ListRepositoriesHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body []repositoryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(body) != 0 {
		t.Errorf("expected 0 repositories, got %d", len(body))
	}
}

func TestListRepositoriesHandler_ReturnsAll(t *testing.T) {
	setupTestDBWithRepos(t)

	repos := []models.Repository{
		{
			Name:       "Repo A",
			Url:        "https://github.com/owner/repo-a",
			Provider:   models.GitHub,
			AuthMethod: models.AuthMethodNone,
			SyncType:   models.SyncTypeManual,
			SyncStatus: models.SyncStatusUnknown,
			CreatedBy:  "user-1",
		},
		{
			Name:       "Repo B",
			Url:        "https://github.com/owner/repo-b",
			Provider:   models.GitHub,
			AuthMethod: models.AuthMethodToken,
			SyncType:   models.SyncTypeManual,
			SyncStatus: models.SyncStatusUnknown,
			CreatedBy:  "user-2",
		},
	}
	for i := range repos {
		if err := db.DB.Select("*").Create(&repos[i]).Error; err != nil {
			t.Fatalf("failed to seed repo: %v", err)
		}
	}

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/repositories", nil)

	ListRepositoriesHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body []repositoryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(body) != 2 {
		t.Errorf("expected 2 repositories, got %d", len(body))
	}
}

func TestCreateRepositoryHandler_InvalidRequest(t *testing.T) {
	setupTestDBWithRepos(t)

	tests := []struct {
		name string
		body any
	}{
		{"empty body", nil},
		{"missing url", map[string]any{"provider": "github", "authMethod": "none", "syncType": "manual"}},
		{"missing provider", map[string]any{"url": "https://github.com/o/r", "authMethod": "none", "syncType": "manual"}},
		{"missing authMethod", map[string]any{"url": "https://github.com/o/r", "provider": "github", "syncType": "manual"}},
		{"missing syncType", map[string]any{"url": "https://github.com/o/r", "provider": "github", "authMethod": "none"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var reqBody []byte
			if tt.body != nil {
				reqBody, _ = json.Marshal(tt.body)
			}

			c, w := makeAuthContext(t, "user-1")
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/repositories", bytes.NewReader(reqBody))
			c.Request.Header.Set("Content-Type", "application/json")

			CreateRepositoryHandler(c)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestCreateRepositoryHandler_InvalidProvider(t *testing.T) {
	setupTestDBWithRepos(t)

	reqBody, _ := json.Marshal(map[string]any{
		"name":       "My Repo",
		"url":        "https://github.com/owner/repo",
		"provider":   "unsupported",
		"authMethod": "none",
		"syncType":   "manual",
	})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/repositories", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	CreateRepositoryHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateRepositoryHandler_InvalidSyncType(t *testing.T) {
	setupTestDBWithRepos(t)

	reqBody, _ := json.Marshal(map[string]any{
		"name":       "My Repo",
		"url":        "https://github.com/owner/repo",
		"provider":   "github",
		"authMethod": "none",
		"syncType":   "invalid",
	})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/repositories", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	CreateRepositoryHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateRepositoryHandler_DefaultPollingInterval(t *testing.T) {
	setupTestDBWithRepos(t)

	reqBody, _ := json.Marshal(map[string]any{
		"name":       "My Repo",
		"url":        "https://github.com/owner/repo",
		"provider":   "github",
		"authMethod": "none",
		"syncType":   "polling",
		// pollingIntervalSeconds is omitted — should default to 60
	})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/repositories", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	CreateRepositoryHandler(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var body repositoryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.PollingIntervalSeconds == nil {
		t.Fatal("expected pollingIntervalSeconds to be set")
	}
	if *body.PollingIntervalSeconds != 60 {
		t.Errorf("expected default pollingIntervalSeconds 60, got %d", *body.PollingIntervalSeconds)
	}
}

func TestCreateRepositoryHandler_InvalidURL(t *testing.T) {
	setupTestDBWithRepos(t)

	reqBody, _ := json.Marshal(map[string]any{
		"name":       "My Repo",
		"url":        "not-a-valid-github-url",
		"provider":   "github",
		"authMethod": "none",
		"syncType":   "manual",
	})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/repositories", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	CreateRepositoryHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateRepositoryHandler_UnsupportedAuthMethod(t *testing.T) {
	setupTestDBWithRepos(t)

	// GitHub only supports none and token; ssh is not supported
	reqBody, _ := json.Marshal(map[string]any{
		"name":       "My Repo",
		"url":        "https://github.com/owner/repo",
		"provider":   "github",
		"authMethod": "ssh",
		"syncType":   "manual",
	})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/repositories", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	CreateRepositoryHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateRepositoryHandler_Success_Manual(t *testing.T) {
	setupTestDBWithRepos(t)

	reqBody, _ := json.Marshal(map[string]any{
		"name":       "My Repo",
		"url":        "https://github.com/owner/my-repo",
		"provider":   "github",
		"authMethod": "none",
		"syncType":   "manual",
	})

	c, w := makeAuthContext(t, "user-123")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/repositories", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	CreateRepositoryHandler(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var body repositoryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Id == "" {
		t.Error("expected non-empty id")
	}
	if body.Name != "owner/my-repo" {
		t.Errorf("expected name %q, got %q", "owner/my-repo", body.Name)
	}
	if body.Url != "https://github.com/owner/my-repo" {
		t.Errorf("unexpected url: %q", body.Url)
	}
	if body.Provider != "github" {
		t.Errorf("expected provider %q, got %q", "github", body.Provider)
	}
	if body.SyncStatus != "unknown" {
		t.Errorf("expected syncStatus %q, got %q", "unknown", body.SyncStatus)
	}
	if body.CreatedBy != "user-123" {
		t.Errorf("expected createdBy %q, got %q", "user-123", body.CreatedBy)
	}
}

func TestCreateRepositoryHandler_Success_Polling(t *testing.T) {
	setupTestDBWithRepos(t)

	interval := int64(300)
	reqBody, _ := json.Marshal(map[string]any{
		"name":                   "Polled Repo",
		"url":                    "https://github.com/owner/polled-repo",
		"provider":               "github",
		"authMethod":             "token",
		"authToken":              "ghp_secret",
		"syncType":               "polling",
		"pollingIntervalSeconds": interval,
	})

	c, w := makeAuthContext(t, "user-456")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/repositories", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	CreateRepositoryHandler(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var body repositoryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.PollingIntervalSeconds == nil {
		t.Fatal("expected pollingIntervalSeconds to be set")
	}
	if *body.PollingIntervalSeconds != 300 {
		t.Errorf("expected pollingIntervalSeconds 300, got %d", *body.PollingIntervalSeconds)
	}

	// Verify token was encrypted in DB (not plaintext)
	repo, err := gorm.G[models.Repository](db.DB).Where("id = ?", body.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to find repo in DB: %v", err)
	}
	if repo.AuthToken == nil {
		t.Fatal("expected authToken to be stored")
	}
	if repo.AuthToken.String() != "ghp_secret" {
		t.Errorf("expected decrypted token %q, got %q", "ghp_secret", repo.AuthToken.String())
	}
}

func mockHTTPClient(statusCode int) func() {
	original := httpclient.Default
	httpclient.Default = &http.Client{
		Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: statusCode,
				Body:       io.NopCloser(strings.NewReader("{}")),
				Header:     make(http.Header),
			}, nil
		}),
	}
	return func() { httpclient.Default = original }
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCreateRepositoryHandler_DuplicateUrlAndSyncType(t *testing.T) {
	setupTestDBWithRepos(t)

	reqBody, _ := json.Marshal(map[string]any{
		"url":        "https://github.com/owner/my-repo",
		"provider":   "github",
		"authMethod": "none",
		"syncType":   "manual",
	})

	// First creation should succeed
	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/repositories", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	CreateRepositoryHandler(c)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 on first create, got %d: %s", w.Code, w.Body.String())
	}

	// Second creation with same URL and syncType should conflict
	reqBody, _ = json.Marshal(map[string]any{
		"url":        "https://github.com/owner/my-repo",
		"provider":   "github",
		"authMethod": "token",
		"authToken":  "ghp_token",
		"syncType":   "manual",
	})
	c, w = makeAuthContext(t, "user-2")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/repositories", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	CreateRepositoryHandler(c)
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 on duplicate url+syncType, got %d: %s", w.Code, w.Body.String())
	}

	// Same URL with a different syncType should succeed
	reqBody, _ = json.Marshal(map[string]any{
		"url":        "https://github.com/owner/my-repo",
		"provider":   "github",
		"authMethod": "none",
		"syncType":   "webhook",
	})
	c, w = makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/repositories", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	CreateRepositoryHandler(c)
	if w.Code != http.StatusCreated {
		t.Errorf("expected 201 for same url with different syncType, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteRepositoryHandler_NotFound(t *testing.T) {
	setupTestDBWithRepos(t)

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/repositories/nonexistent-id", nil)
	c.Params = gin.Params{{Key: "id", Value: "nonexistent-id"}}

	DeleteRepositoryHandler(c)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteRepositoryHandler_Success(t *testing.T) {
	setupTestDBWithRepos(t)

	repo := models.Repository{
		Name:       "To Delete",
		Url:        "https://github.com/owner/to-delete",
		Provider:   models.GitHub,
		AuthMethod: models.AuthMethodNone,
		SyncType:   models.SyncTypeManual,
		SyncStatus: models.SyncStatusUnknown,
		CreatedBy:  "user-1",
	}
	if err := db.DB.Select("*").Create(&repo).Error; err != nil {
		t.Fatalf("failed to seed repo: %v", err)
	}

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/repositories/"+repo.Id, nil)
	c.Params = gin.Params{{Key: "id", Value: repo.Id}}

	DeleteRepositoryHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify the record is gone
	var count int64
	db.DB.Model(&models.Repository{}).Where("id = ?", repo.Id).Count(&count)
	if count != 0 {
		t.Error("expected repository to be deleted from DB")
	}
}

func TestTestConnectionHandler_InvalidRequest(t *testing.T) {
	setupTestDBWithRepos(t)

	tests := []struct {
		name string
		body any
	}{
		{"empty body", nil},
		{"missing url", map[string]any{"provider": "github", "authMethod": "none"}},
		{"missing provider", map[string]any{"url": "https://github.com/o/r", "authMethod": "none"}},
		{"missing authMethod", map[string]any{"url": "https://github.com/o/r", "provider": "github"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var reqBody []byte
			if tt.body != nil {
				reqBody, _ = json.Marshal(tt.body)
			}

			c, w := makeAuthContext(t, "user-1")
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/repositories/test-connection", bytes.NewReader(reqBody))
			c.Request.Header.Set("Content-Type", "application/json")

			TestConnectionHandler(c)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestTestConnectionHandler_InvalidProvider(t *testing.T) {
	setupTestDBWithRepos(t)

	reqBody, _ := json.Marshal(map[string]any{
		"url":        "https://github.com/owner/repo",
		"provider":   "unsupported",
		"authMethod": "none",
	})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/repositories/test-connection", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	TestConnectionHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTestConnectionHandler_InvalidURL(t *testing.T) {
	setupTestDBWithRepos(t)

	reqBody, _ := json.Marshal(map[string]any{
		"url":        "not-a-valid-url",
		"provider":   "github",
		"authMethod": "none",
	})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/repositories/test-connection", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	TestConnectionHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTestConnectionHandler_ConnectionFailed(t *testing.T) {
	setupTestDBWithRepos(t)
	restore := mockHTTPClient(http.StatusNotFound)
	t.Cleanup(restore)

	reqBody, _ := json.Marshal(map[string]any{
		"url":        "https://github.com/owner/repo",
		"provider":   "github",
		"authMethod": "none",
	})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/repositories/test-connection", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	TestConnectionHandler(c)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d: %s", w.Code, w.Body.String())
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["error"] == "" {
		t.Error("expected non-empty error message")
	}
}

func TestUpdateRepositoryHandler_NotFound(t *testing.T) {
	setupTestDBWithRepos(t)

	reqBody, _ := json.Marshal(map[string]any{
		"url":        "https://github.com/owner/repo",
		"authMethod": "none",
		"syncType":   "manual",
	})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPatch, "/api/v1/repositories/nonexistent-id", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: "nonexistent-id"}}

	UpdateRepositoryHandler(c)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateRepositoryHandler_InvalidRequest(t *testing.T) {
	tests := []struct {
		name string
		body any
	}{
		{"empty body", nil},
		{"missing url", map[string]any{"authMethod": "none", "syncType": "manual"}},
		{"missing authMethod", map[string]any{"url": "https://github.com/o/r", "syncType": "manual"}},
		{"missing syncType", map[string]any{"url": "https://github.com/o/r", "authMethod": "none"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupTestDBWithRepos(t)
			repo := models.Repository{
				Name:       "Test Repo",
				Url:        "https://github.com/owner/repo",
				Provider:   models.GitHub,
				AuthMethod: models.AuthMethodNone,
				SyncType:   models.SyncTypeManual,
				SyncStatus: models.SyncStatusUnknown,
				CreatedBy:  "user-1",
			}
			if err := db.DB.Select("*").Create(&repo).Error; err != nil {
				t.Fatalf("failed to seed repo: %v", err)
			}

			var reqBody []byte
			if tt.body != nil {
				reqBody, _ = json.Marshal(tt.body)
			}

			c, w := makeAuthContext(t, "user-1")
			c.Request = httptest.NewRequest(http.MethodPatch, "/api/v1/repositories/"+repo.Id, bytes.NewReader(reqBody))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Params = gin.Params{{Key: "id", Value: repo.Id}}

			UpdateRepositoryHandler(c)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestUpdateRepositoryHandler_InvalidSyncType(t *testing.T) {
	setupTestDBWithRepos(t)

	repo := models.Repository{
		Name:       "Test Repo",
		Url:        "https://github.com/owner/repo",
		Provider:   models.GitHub,
		AuthMethod: models.AuthMethodNone,
		SyncType:   models.SyncTypeManual,
		SyncStatus: models.SyncStatusUnknown,
		CreatedBy:  "user-1",
	}
	if err := db.DB.Select("*").Create(&repo).Error; err != nil {
		t.Fatalf("failed to seed repo: %v", err)
	}

	reqBody, _ := json.Marshal(map[string]any{
		"url":        "https://github.com/owner/repo",
		"authMethod": "none",
		"syncType":   "invalid",
	})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPatch, "/api/v1/repositories/"+repo.Id, bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: repo.Id}}

	UpdateRepositoryHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateRepositoryHandler_UnsupportedAuthMethod(t *testing.T) {
	setupTestDBWithRepos(t)

	repo := models.Repository{
		Name:       "Test Repo",
		Url:        "https://github.com/owner/repo",
		Provider:   models.GitHub,
		AuthMethod: models.AuthMethodNone,
		SyncType:   models.SyncTypeManual,
		SyncStatus: models.SyncStatusUnknown,
		CreatedBy:  "user-1",
	}
	if err := db.DB.Select("*").Create(&repo).Error; err != nil {
		t.Fatalf("failed to seed repo: %v", err)
	}

	// GitHub does not support SSH
	reqBody, _ := json.Marshal(map[string]any{
		"url":        "https://github.com/owner/repo",
		"authMethod": "ssh",
		"syncType":   "manual",
	})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPatch, "/api/v1/repositories/"+repo.Id, bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: repo.Id}}

	UpdateRepositoryHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateRepositoryHandler_InvalidURL(t *testing.T) {
	setupTestDBWithRepos(t)

	repo := models.Repository{
		Name:       "Test Repo",
		Url:        "https://github.com/owner/repo",
		Provider:   models.GitHub,
		AuthMethod: models.AuthMethodNone,
		SyncType:   models.SyncTypeManual,
		SyncStatus: models.SyncStatusUnknown,
		CreatedBy:  "user-1",
	}
	if err := db.DB.Select("*").Create(&repo).Error; err != nil {
		t.Fatalf("failed to seed repo: %v", err)
	}

	reqBody, _ := json.Marshal(map[string]any{
		"url":        "not-a-valid-github-url",
		"authMethod": "none",
		"syncType":   "manual",
	})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPatch, "/api/v1/repositories/"+repo.Id, bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: repo.Id}}

	UpdateRepositoryHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateRepositoryHandler_Success(t *testing.T) {
	setupTestDBWithRepos(t)

	repo := models.Repository{
		Name:       "owner/old-repo",
		Url:        "https://github.com/owner/old-repo",
		Provider:   models.GitHub,
		AuthMethod: models.AuthMethodNone,
		SyncType:   models.SyncTypeManual,
		SyncStatus: models.SyncStatusUnknown,
		CreatedBy:  "user-1",
	}
	if err := db.DB.Select("*").Create(&repo).Error; err != nil {
		t.Fatalf("failed to seed repo: %v", err)
	}

	interval := int64(120)
	reqBody, _ := json.Marshal(map[string]any{
		"url":                    "https://github.com/owner/new-repo",
		"authMethod":             "token",
		"authToken":              "test-token", //nolint:gosec
		"syncType":               "polling",
		"pollingIntervalSeconds": interval,
	})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPatch, "/api/v1/repositories/"+repo.Id, bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: repo.Id}}

	UpdateRepositoryHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body repositoryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Url != "https://github.com/owner/new-repo" {
		t.Errorf("expected url %q, got %q", "https://github.com/owner/new-repo", body.Url)
	}
	if body.Name != "owner/new-repo" {
		t.Errorf("expected name %q, got %q", "owner/new-repo", body.Name)
	}
	if body.AuthMethod != "token" {
		t.Errorf("expected authMethod %q, got %q", "token", body.AuthMethod)
	}
	if body.SyncType != "polling" {
		t.Errorf("expected syncType %q, got %q", "polling", body.SyncType)
	}
	if body.PollingIntervalSeconds == nil || *body.PollingIntervalSeconds != 120 {
		t.Errorf("expected pollingIntervalSeconds 120, got %v", body.PollingIntervalSeconds)
	}
}

func TestUpdateRepositoryHandler_SwitchToWebhook(t *testing.T) {
	setupTestDBWithRepos(t)

	repo := models.Repository{
		Name:       "owner/repo",
		Url:        "https://github.com/owner/repo",
		Provider:   models.GitHub,
		AuthMethod: models.AuthMethodNone,
		SyncType:   models.SyncTypeManual,
		SyncStatus: models.SyncStatusUnknown,
		CreatedBy:  "user-1",
	}
	if err := db.DB.Select("*").Create(&repo).Error; err != nil {
		t.Fatalf("failed to seed repo: %v", err)
	}

	reqBody, _ := json.Marshal(map[string]any{
		"url":        "https://github.com/owner/repo",
		"authMethod": "none",
		"syncType":   "webhook",
	})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPatch, "/api/v1/repositories/"+repo.Id, bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: repo.Id}}

	UpdateRepositoryHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body repositoryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.SyncType != "webhook" {
		t.Errorf("expected syncType %q, got %q", "webhook", body.SyncType)
	}
	if body.WebhookSecret == nil || *body.WebhookSecret == "" {
		t.Error("expected webhookSecret to be returned when switching to webhook")
	}
}

func TestUpdateRepositoryHandler_SwitchFromWebhook(t *testing.T) {
	setupTestDBWithRepos(t)

	secret := crypto.EncryptedString("existing-secret")
	repo := models.Repository{
		Name:          "owner/repo",
		Url:           "https://github.com/owner/repo",
		Provider:      models.GitHub,
		AuthMethod:    models.AuthMethodNone,
		SyncType:      models.SyncTypeWebhook,
		SyncStatus:    models.SyncStatusUnknown,
		WebhookSecret: &secret,
		CreatedBy:     "user-1",
	}
	if err := db.DB.Select("*").Create(&repo).Error; err != nil {
		t.Fatalf("failed to seed repo: %v", err)
	}

	reqBody, _ := json.Marshal(map[string]any{
		"url":        "https://github.com/owner/repo",
		"authMethod": "none",
		"syncType":   "manual",
	})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPatch, "/api/v1/repositories/"+repo.Id, bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: repo.Id}}

	UpdateRepositoryHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body repositoryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.SyncType != "manual" {
		t.Errorf("expected syncType %q, got %q", "manual", body.SyncType)
	}
	if body.WebhookSecret != nil {
		t.Error("expected webhookSecret to be nil after switching away from webhook")
	}

	// Confirm webhook secret is cleared in DB
	updated, err := gorm.G[models.Repository](db.DB).Where("id = ?", repo.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to fetch repo from DB: %v", err)
	}
	if updated.WebhookSecret != nil {
		t.Error("expected webhookSecret to be cleared in DB")
	}
}

func TestTestConnectionHandler_Success(t *testing.T) {
	setupTestDBWithRepos(t)
	restore := mockHTTPClient(http.StatusOK)
	t.Cleanup(restore)

	reqBody, _ := json.Marshal(map[string]any{
		"url":        "https://github.com/owner/repo",
		"provider":   "github",
		"authMethod": "token",
		"authToken":  "ghp_mytoken",
	})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/repositories/test-connection", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	TestConnectionHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["message"] != "connection successful" {
		t.Errorf("expected message %q, got %q", "connection successful", body["message"])
	}
}
