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
		{"missing name", map[string]any{"url": "https://github.com/o/r", "provider": "github", "authMethod": "none", "syncType": "manual"}},
		{"missing url", map[string]any{"name": "My Repo", "provider": "github", "authMethod": "none", "syncType": "manual"}},
		{"missing provider", map[string]any{"name": "My Repo", "url": "https://github.com/o/r", "authMethod": "none", "syncType": "manual"}},
		{"missing authMethod", map[string]any{"name": "My Repo", "url": "https://github.com/o/r", "provider": "github", "syncType": "manual"}},
		{"missing syncType", map[string]any{"name": "My Repo", "url": "https://github.com/o/r", "provider": "github", "authMethod": "none"}},
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

func TestCreateRepositoryHandler_MissingPollingInterval(t *testing.T) {
	setupTestDBWithRepos(t)

	reqBody, _ := json.Marshal(map[string]any{
		"name":       "My Repo",
		"url":        "https://github.com/owner/repo",
		"provider":   "github",
		"authMethod": "none",
		"syncType":   "polling",
		// pollingIntervalSeconds is missing
	})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/repositories", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	CreateRepositoryHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
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
	if body.Name != "My Repo" {
		t.Errorf("expected name %q, got %q", "My Repo", body.Name)
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
