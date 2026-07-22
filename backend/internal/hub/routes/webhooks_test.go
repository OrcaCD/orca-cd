package routes

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/applications"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/repositories"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// mockRepoProvider is a minimal Provider for testing enqueueGenericApps.
type mockRepoProvider struct {
	getLatestCommitCalls []string
	latestCommitResult   repositories.CommitInfo
	latestCommitErr      error
	onGetLatestCommit    func()
}

func (m *mockRepoProvider) ParseURL(_ string) (string, string, error)                    { return "", "", nil }
func (m *mockRepoProvider) SupportedAuthMethods() []models.RepositoryAuthMethod          { return nil }
func (m *mockRepoProvider) TestConnection(_ context.Context, _ *models.Repository) error { return nil }
func (m *mockRepoProvider) ListBranches(_ context.Context, _ *models.Repository) ([]string, error) {
	return nil, nil
}
func (m *mockRepoProvider) ListTree(_ context.Context, _ *models.Repository, _ string) ([]repositories.TreeEntry, error) {
	return nil, nil
}
func (m *mockRepoProvider) GetFileContent(_ context.Context, _ *models.Repository, _ string, _ string) (string, error) {
	return "", nil
}
func (m *mockRepoProvider) GetLatestCommit(_ context.Context, _ *models.Repository, branch string) (repositories.CommitInfo, error) {
	if m.onGetLatestCommit != nil {
		m.onGetLatestCommit()
	}
	m.getLatestCommitCalls = append(m.getLatestCommitCalls, branch)
	return m.latestCommitResult, m.latestCommitErr
}

func setupTestDBWithWebhookRepos(t *testing.T) {
	t.Helper()
	setupTestDBWithRepos(t)
	nop := zerolog.Nop()
	applications.DefaultQueue = applications.NewQueue(&nop)
	applications.DefaultQueue.Start()
	t.Cleanup(func() { applications.DefaultQueue = nil })
}

// githubSig computes the X-Hub-Signature-256 header value for a given secret and body.
func githubSig(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// giteaSig computes the X-Gitea-Signature header value (no prefix).
func giteaSig(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return hex.EncodeToString(mac.Sum(nil))
}

func seedWebhookRepo(t *testing.T, provider models.RepositoryProvider, secret string) models.Repository {
	t.Helper()
	encSecret := crypto.EncryptedString(secret)
	var repoURL string
	switch provider {
	case models.GitLab:
		repoURL = fmt.Sprintf("https://gitlab.com/owner/repo-%s", provider)
	case models.Gitea:
		repoURL = fmt.Sprintf("https://gitea.example.com/owner/repo-%s", provider)
	default:
		repoURL = fmt.Sprintf("https://github.com/owner/repo-%s", provider)
	}
	repo := models.Repository{
		Name:          "owner/repo",
		Url:           repoURL,
		Provider:      provider,
		AuthMethod:    models.AuthMethodNone,
		SyncType:      models.SyncTypeWebhook,
		SyncStatus:    models.SyncStatusUnknown,
		WebhookSecret: &encSecret,
		CreatedBy:     "user-1",
	}
	if err := db.DB.Select("*").Create(&repo).Error; err != nil {
		t.Fatalf("failed to seed webhook repo: %v", err)
	}
	return repo
}

func makeWebhookRequest(repoID, body string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/repositories/"+repoID, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: repoID}}
	return c, w
}

func TestWebhookHandler_NotFound(t *testing.T) {
	setupTestDBWithWebhookRepos(t)

	c, w := makeWebhookRequest("nonexistent-id", `{}`)
	WebhookHandler(c)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWebhookHandler_WrongSyncType(t *testing.T) {
	setupTestDBWithWebhookRepos(t)

	repo := models.Repository{
		Name:       "owner/repo",
		Url:        "https://github.com/owner/repo-polling",
		Provider:   models.GitHub,
		AuthMethod: models.AuthMethodNone,
		SyncType:   models.SyncTypePolling,
		SyncStatus: models.SyncStatusUnknown,
		CreatedBy:  "user-1",
	}
	if err := db.DB.Select("*").Create(&repo).Error; err != nil {
		t.Fatalf("failed to seed repo: %v", err)
	}

	c, w := makeWebhookRequest(repo.Id, `{}`)
	WebhookHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWebhookHandler_GitHub_InvalidSignature(t *testing.T) {
	setupTestDBWithWebhookRepos(t)
	repo := seedWebhookRepo(t, models.GitHub, "mysecret")

	c, w := makeWebhookRequest(repo.Id, `{"ref":"refs/heads/main"}`)
	c.Request.Header.Set("X-GitHub-Event", "push")
	c.Request.Header.Set("X-Hub-Signature-256", "sha256=badhex")
	WebhookHandler(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWebhookHandler_GitHub_MissingSignature(t *testing.T) {
	setupTestDBWithWebhookRepos(t)
	repo := seedWebhookRepo(t, models.GitHub, "mysecret")

	c, w := makeWebhookRequest(repo.Id, `{"ref":"refs/heads/main"}`)
	c.Request.Header.Set("X-GitHub-Event", "push")
	WebhookHandler(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWebhookHandler_GitHub_NonPushEvent(t *testing.T) {
	setupTestDBWithWebhookRepos(t)
	const secret = "mysecret"
	const body = `{"action":"opened"}`
	repo := seedWebhookRepo(t, models.GitHub, secret)

	c, w := makeWebhookRequest(repo.Id, body)
	c.Request.Header.Set("X-GitHub-Event", "ping")
	c.Request.Header.Set("X-Hub-Signature-256", githubSig(secret, body))
	WebhookHandler(c)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	// Sync status must not have changed
	var got models.Repository
	if err := db.DB.First(&got, "id = ?", repo.Id).Error; err != nil {
		t.Fatalf("failed to load repo: %v", err)
	}
	if got.SyncStatus != models.SyncStatusUnknown {
		t.Errorf("expected sync_status unchanged (%s), got %s", models.SyncStatusUnknown, got.SyncStatus)
	}
}

func TestWebhookHandler_GitHub_PushEvent_UpdatesDB(t *testing.T) {
	setupTestDBWithWebhookRepos(t)
	const secret = "mysecret"
	const body = `{"ref":"refs/heads/main","after":"sha-after"}`
	repo := seedWebhookRepo(t, models.GitHub, secret)

	c, w := makeWebhookRequest(repo.Id, body)
	c.Request.Header.Set("X-GitHub-Event", "push")
	c.Request.Header.Set("X-Hub-Signature-256", githubSig(secret, body))
	WebhookHandler(c)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var got models.Repository
	if err := db.DB.First(&got, "id = ?", repo.Id).Error; err != nil {
		t.Fatalf("failed to reload repo: %v", err)
	}
	if got.SyncStatus != models.SyncStatusSuccess {
		t.Errorf("expected sync_status %s, got %s", models.SyncStatusSuccess, got.SyncStatus)
	}
	if got.LastSyncedAt == nil {
		t.Error("expected last_synced_at to be set")
	}
}

func TestWebhookHandler_GitHub_PushEvent_DeduplicatesConcurrentDelivery(t *testing.T) {
	setupTestDBWithWebhookRepos(t)
	nop := zerolog.Nop()
	applications.DefaultPoller = applications.NewPoller(&nop)
	t.Cleanup(func() { applications.DefaultPoller = nil })

	const secret = "mysecret"
	const body = `{"ref":"refs/heads/main","after":"sha-after"}`
	repo := seedWebhookRepo(t, models.GitHub, secret)

	// Simulate a sync for this repository already in flight, e.g. from the
	// original delivery of a webhook the provider is now retrying.
	release, ok := applications.DefaultPoller.TryLockRepositorySync(repo.Id)
	if !ok {
		t.Fatal("failed to pre-lock repository sync slot")
	}
	defer release()

	c, w := makeWebhookRequest(repo.Id, body)
	c.Request.Header.Set("X-GitHub-Event", "push")
	c.Request.Header.Set("X-Hub-Signature-256", githubSig(secret, body))
	WebhookHandler(c)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var got models.Repository
	if err := db.DB.First(&got, "id = ?", repo.Id).Error; err != nil {
		t.Fatalf("failed to reload repo: %v", err)
	}
	if got.SyncStatus != models.SyncStatusUnknown {
		t.Errorf("expected duplicate delivery to be skipped while a sync is in progress, got sync_status %s", got.SyncStatus)
	}
}

func TestWebhookHandler_Gitea_InvalidSignature(t *testing.T) {
	setupTestDBWithWebhookRepos(t)
	repo := seedWebhookRepo(t, models.Gitea, "mysecret")

	c, w := makeWebhookRequest(repo.Id, `{"ref":"refs/heads/main"}`)
	c.Request.Header.Set("X-Gitea-Event", "push")
	c.Request.Header.Set("X-Gitea-Signature", "badhex")
	WebhookHandler(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWebhookHandler_Gitea_PushEvent_UpdatesDB(t *testing.T) {
	setupTestDBWithWebhookRepos(t)
	const secret = "giteasecret"
	const body = `{"ref":"refs/heads/main","after":"sha-after"}`
	repo := seedWebhookRepo(t, models.Gitea, secret)

	c, w := makeWebhookRequest(repo.Id, body)
	c.Request.Header.Set("X-Gitea-Event", "push")
	c.Request.Header.Set("X-Gitea-Signature", giteaSig(secret, body))
	WebhookHandler(c)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var got models.Repository
	if err := db.DB.First(&got, "id = ?", repo.Id).Error; err != nil {
		t.Fatalf("failed to reload repo: %v", err)
	}
	if got.SyncStatus != models.SyncStatusSuccess {
		t.Errorf("expected sync_status %s, got %s", models.SyncStatusSuccess, got.SyncStatus)
	}
	if got.LastSyncedAt == nil {
		t.Error("expected last_synced_at to be set")
	}
}

func TestWebhookHandler_GitLab_InvalidToken(t *testing.T) {
	setupTestDBWithWebhookRepos(t)
	repo := seedWebhookRepo(t, models.GitLab, "mysecret")

	c, w := makeWebhookRequest(repo.Id, `{"ref":"refs/heads/main"}`)
	c.Request.Header.Set("X-Gitlab-Event", "Push Hook")
	c.Request.Header.Set("X-Gitlab-Token", "wrongsecret")
	WebhookHandler(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWebhookHandler_GitLab_PushEvent_UpdatesDB(t *testing.T) {
	setupTestDBWithWebhookRepos(t)
	const secret = "gitlabsecret"
	const body = `{"ref":"refs/heads/main","after":"sha-after"}`
	repo := seedWebhookRepo(t, models.GitLab, secret)

	c, w := makeWebhookRequest(repo.Id, body)
	c.Request.Header.Set("X-Gitlab-Event", "Push Hook")
	c.Request.Header.Set("X-Gitlab-Token", secret)
	WebhookHandler(c)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var got models.Repository
	if err := db.DB.First(&got, "id = ?", repo.Id).Error; err != nil {
		t.Fatalf("failed to reload repo: %v", err)
	}
	if got.SyncStatus != models.SyncStatusSuccess {
		t.Errorf("expected sync_status %s, got %s", models.SyncStatusSuccess, got.SyncStatus)
	}
	if got.LastSyncedAt == nil {
		t.Error("expected last_synced_at to be set")
	}
}

func TestWebhookHandler_UnsupportedProvider_Rejected(t *testing.T) {
	setupTestDBWithWebhookRepos(t)
	// Generic provider has no signature scheme defined
	encSecret := crypto.EncryptedString("somesecret")
	repo := models.Repository{
		Name:          "owner/repo",
		Url:           "https://generic.example.com/owner/repo",
		Provider:      models.Generic,
		AuthMethod:    models.AuthMethodNone,
		SyncType:      models.SyncTypeWebhook,
		SyncStatus:    models.SyncStatusUnknown,
		WebhookSecret: &encSecret,
		CreatedBy:     "user-1",
	}
	if err := db.DB.Select("*").Create(&repo).Error; err != nil {
		t.Fatalf("failed to seed repo: %v", err)
	}

	c, w := makeWebhookRequest(repo.Id, `{}`)
	WebhookHandler(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestValidateHMACSHA256_Valid(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/main"}`)
	mac := hmac.New(sha256.New, []byte("secret"))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))

	if !validateHMACSHA256("secret", body, sig) {
		t.Error("expected valid signature to pass")
	}
}

func TestValidateHMACSHA256_Invalid(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/main"}`)
	if validateHMACSHA256("secret", body, "deadbeef") {
		t.Error("expected invalid signature to fail")
	}
}

func TestValidateHMACSHA256_Empty(t *testing.T) {
	if validateHMACSHA256("secret", []byte("body"), "") {
		t.Error("expected empty signature to fail")
	}
}

func TestValidateHMACSHA256_WrongSecret(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/main"}`)
	mac := hmac.New(sha256.New, []byte("correct-secret"))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))

	if validateHMACSHA256("wrong-secret", body, sig) {
		t.Error("expected wrong secret to fail")
	}
}

func TestIsPushEvent(t *testing.T) {
	tests := []struct {
		provider models.RepositoryProvider
		header   string
		value    string
		want     bool
	}{
		{models.GitHub, "X-GitHub-Event", "push", true},
		{models.GitHub, "X-GitHub-Event", "ping", false},
		{models.GitHub, "X-GitHub-Event", "", false},
		{models.Gitea, "X-Gitea-Event", "push", true},
		{models.Gitea, "X-Gitea-Event", "issues", false},
		{models.GitLab, "X-Gitlab-Event", "Push Hook", true},
		{models.GitLab, "X-Gitlab-Event", "Merge Request Hook", false},
		{models.Generic, "", "", false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%s", tt.provider, tt.value), func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			if tt.header != "" {
				c.Request.Header.Set(tt.header, tt.value)
			}
			got := isPushEvent(c, tt.provider)
			if got != tt.want {
				t.Errorf("isPushEvent(%s, %q) = %v, want %v", tt.provider, tt.value, got, tt.want)
			}
		})
	}
}

func TestValidateSignature_GitHub_Valid(t *testing.T) {
	const secret = "s3cr3t"
	body := []byte(`{"ref":"refs/heads/main"}`)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body)))
	c.Request.Header.Set("X-Hub-Signature-256", githubSig(secret, string(body)))

	if !validateSignature(c, models.GitHub, secret, body) {
		t.Error("expected valid GitHub signature to pass")
	}
}

func TestValidateSignature_GitLab_Valid(t *testing.T) {
	const secret = "s3cr3t"
	body := []byte(`{"ref":"refs/heads/main"}`)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body)))
	c.Request.Header.Set("X-Gitlab-Token", secret)

	if !validateSignature(c, models.GitLab, secret, body) {
		t.Error("expected valid GitLab token to pass")
	}
}

func TestValidateSignature_GitLab_Invalid(t *testing.T) {
	body := []byte(`{}`)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body)))
	c.Request.Header.Set("X-Gitlab-Token", "wrongtoken")

	if validateSignature(c, models.GitLab, "correcttoken", body) {
		t.Error("expected wrong GitLab token to fail")
	}
}

func TestValidateSignature_UnknownProvider(t *testing.T) {
	body := []byte(`{}`)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body)))

	if validateSignature(c, models.Generic, "secret", body) {
		t.Error("expected unknown provider to fail")
	}
}

func TestWebhookHandler_NilSecret_Returns500(t *testing.T) {
	setupTestDBWithWebhookRepos(t)

	repo := models.Repository{
		Name:       "owner/repo",
		Url:        "https://github.com/owner/repo-nosecret",
		Provider:   models.GitHub,
		AuthMethod: models.AuthMethodNone,
		SyncType:   models.SyncTypeWebhook,
		SyncStatus: models.SyncStatusUnknown,
		CreatedBy:  "user-1",
	}
	if err := db.DB.Select("*").Create(&repo).Error; err != nil {
		t.Fatalf("failed to seed repo: %v", err)
	}
	// Clear the secret directly in the DB
	if err := db.DB.Model(&models.Repository{}).Where("id = ?", repo.Id).Update("webhook_secret", gorm.Expr("NULL")).Error; err != nil {
		t.Fatalf("failed to clear secret: %v", err)
	}

	c, w := makeWebhookRequest(repo.Id, `{}`)
	WebhookHandler(c)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestParseWebhookPushDetails_GitHub_JSON(t *testing.T) {
	body := `{"ref":"refs/heads/main","after":"sha-after"}`

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	details, err := parseWebhookPushDetails(c, []byte(body))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if details.Branch != "main" {
		t.Errorf("expected branch main, got %q", details.Branch)
	}
	if details.Commit != "sha-after" {
		t.Errorf("expected commit sha-after, got %q", details.Commit)
	}
}

func TestParseWebhookPushDetails_GitHub_FormPayload(t *testing.T) {
	payload := `{"ref":"refs/heads/release","after":"sha-after"}`
	formBody := url.Values{"payload": {payload}}.Encode()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(formBody))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	details, err := parseWebhookPushDetails(c, []byte(formBody))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if details.Branch != "release" {
		t.Errorf("expected branch release, got %q", details.Branch)
	}
	if details.Commit != "sha-after" {
		t.Errorf("expected commit sha-after, got %q", details.Commit)
	}
}

func TestParseWebhookPushDetails_Gitea_JSON(t *testing.T) {
	body := `{"ref":"refs/heads/main","after":"sha-after"}`

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	details, err := parseWebhookPushDetails(c, []byte(body))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if details.Branch != "main" {
		t.Errorf("expected branch main, got %q", details.Branch)
	}
	if details.Commit != "sha-after" {
		t.Errorf("expected commit sha-after, got %q", details.Commit)
	}
}

func TestParseWebhookPushDetails_Gitea_FormPayload(t *testing.T) {
	payload := `{"ref":"refs/heads/hotfix","after":"sha-after"}`
	formBody := url.Values{"payload": {payload}}.Encode()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(formBody))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	details, err := parseWebhookPushDetails(c, []byte(formBody))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if details.Branch != "hotfix" {
		t.Errorf("expected branch hotfix, got %q", details.Branch)
	}
	if details.Commit != "sha-after" {
		t.Errorf("expected commit sha-after, got %q", details.Commit)
	}
}

func TestParseWebhookPushDetails_GitLab_JSON(t *testing.T) {
	body := `{"ref":"refs/heads/dev","after":"sha-after"}`

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	details, err := parseWebhookPushDetails(c, []byte(body))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if details.Branch != "dev" {
		t.Errorf("expected branch dev, got %q", details.Branch)
	}
	if details.Commit != "sha-after" {
		t.Errorf("expected commit sha-after, got %q", details.Commit)
	}
}

func TestParseWebhookPushDetails_GitLab_FormFields(t *testing.T) {
	formBody := url.Values{
		"ref":   {"refs/heads/feature/login"},
		"after": {"sha-after"},
	}.Encode()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(formBody))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	details, err := parseWebhookPushDetails(c, []byte(formBody))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if details.Branch != "feature/login" {
		t.Errorf("expected branch feature/login, got %q", details.Branch)
	}
	if details.Commit != "sha-after" {
		t.Errorf("expected commit sha-after, got %q", details.Commit)
	}
}

func TestParseWebhookPushDetails_EmptyBody_NonForm_ReturnsError(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
	c.Request.Header.Set("Content-Type", "application/json")

	_, err := parseWebhookPushDetails(c, []byte{})
	if err == nil {
		t.Fatal("expected error for empty non-form body, got nil")
	}
}

func TestParseWebhookPushDetails_MissingRef_ReturnsError(t *testing.T) {
	body := `{"after":"sha-after"}`

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	_, err := parseWebhookPushDetails(c, []byte(body))
	if err == nil {
		t.Fatal("expected error for missing ref, got nil")
	}
}

func TestParseWebhookPushDetails_MissingAfter_ReturnsError(t *testing.T) {
	body := `{"ref":"refs/heads/main"}`

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	_, err := parseWebhookPushDetails(c, []byte(body))
	if err == nil {
		t.Fatal("expected error for missing commit hash, got nil")
	}
}

func TestWebhookHandler_Generic_NoAuth_Returns401(t *testing.T) {
	setupTestDBWithWebhookRepos(t)
	repo := seedWebhookRepo(t, models.GitHub, "mysecret")

	c, w := makeWebhookRequest(repo.Id, "")
	// No provider event header, no Authorization header
	WebhookHandler(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWebhookHandler_Generic_WrongToken_Returns401(t *testing.T) {
	setupTestDBWithWebhookRepos(t)
	repo := seedWebhookRepo(t, models.GitHub, "mysecret")

	c, w := makeWebhookRequest(repo.Id, "")
	c.Request.Header.Set("Authorization", "Bearer wrongtoken")
	WebhookHandler(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWebhookHandler_Generic_EmptyBody_UpdatesDB(t *testing.T) {
	setupTestDBWithWebhookRepos(t)
	const secret = "mysecret"
	repo := seedWebhookRepo(t, models.GitHub, secret)

	c, w := makeWebhookRequest(repo.Id, "")
	c.Request.Header.Set("Authorization", "Bearer "+secret)
	WebhookHandler(c)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var got models.Repository
	if err := db.DB.First(&got, "id = ?", repo.Id).Error; err != nil {
		t.Fatalf("failed to reload repo: %v", err)
	}
	if got.SyncStatus != models.SyncStatusSuccess {
		t.Errorf("expected sync_status %s, got %s", models.SyncStatusSuccess, got.SyncStatus)
	}
	if got.LastSyncedAt == nil {
		t.Error("expected last_synced_at to be set")
	}
}

func TestWebhookHandler_Generic_DeduplicatesConcurrentDelivery(t *testing.T) {
	setupTestDBWithWebhookRepos(t)
	nop := zerolog.Nop()
	applications.DefaultPoller = applications.NewPoller(&nop)
	t.Cleanup(func() { applications.DefaultPoller = nil })

	const secret = "mysecret"
	repo := seedWebhookRepo(t, models.GitHub, secret)

	release, ok := applications.DefaultPoller.TryLockRepositorySync(repo.Id)
	if !ok {
		t.Fatal("failed to pre-lock repository sync slot")
	}
	defer release()

	c, w := makeWebhookRequest(repo.Id, "")
	c.Request.Header.Set("Authorization", "Bearer "+secret)
	WebhookHandler(c)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var got models.Repository
	if err := db.DB.First(&got, "id = ?", repo.Id).Error; err != nil {
		t.Fatalf("failed to reload repo: %v", err)
	}
	if got.SyncStatus != models.SyncStatusUnknown {
		t.Errorf("expected duplicate delivery to be skipped while a sync is in progress, got sync_status %s", got.SyncStatus)
	}
}

func TestWebhookHandler_Generic_WithBranch_UpdatesDB(t *testing.T) {
	setupTestDBWithWebhookRepos(t)
	const secret = "mysecret"
	repo := seedWebhookRepo(t, models.GitHub, secret)

	c, w := makeWebhookRequest(repo.Id, `{"branch":"main"}`)
	c.Request.Header.Set("Authorization", "Bearer "+secret)
	WebhookHandler(c)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var got models.Repository
	if err := db.DB.First(&got, "id = ?", repo.Id).Error; err != nil {
		t.Fatalf("failed to reload repo: %v", err)
	}
	if got.SyncStatus != models.SyncStatusSuccess {
		t.Errorf("expected sync_status %s, got %s", models.SyncStatusSuccess, got.SyncStatus)
	}
}

func TestWebhookHandler_Generic_WithRef_UpdatesDB(t *testing.T) {
	setupTestDBWithWebhookRepos(t)
	const secret = "mysecret"
	repo := seedWebhookRepo(t, models.GitHub, secret)

	c, w := makeWebhookRequest(repo.Id, `{"ref":"refs/heads/main","commit":"abc123"}`)
	c.Request.Header.Set("Authorization", "Bearer "+secret)
	WebhookHandler(c)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var got models.Repository
	if err := db.DB.First(&got, "id = ?", repo.Id).Error; err != nil {
		t.Fatalf("failed to reload repo: %v", err)
	}
	if got.SyncStatus != models.SyncStatusSuccess {
		t.Errorf("expected sync_status %s, got %s", models.SyncStatusSuccess, got.SyncStatus)
	}
}

func TestParseGenericWebhookBody_EmptyBody(t *testing.T) {
	branch, commit := parseGenericWebhookBody([]byte{})
	if branch != "" || commit != "" {
		t.Errorf("expected empty strings, got branch=%q commit=%q", branch, commit)
	}
}

func TestParseGenericWebhookBody_InvalidJSON(t *testing.T) {
	branch, commit := parseGenericWebhookBody([]byte("not-json"))
	if branch != "" || commit != "" {
		t.Errorf("expected empty strings on invalid JSON, got branch=%q commit=%q", branch, commit)
	}
}

func TestParseGenericWebhookBody_WithRef(t *testing.T) {
	branch, commit := parseGenericWebhookBody([]byte(`{"ref":"refs/heads/feature","commit":"sha1"}`))
	if branch != "feature" {
		t.Errorf("expected branch=feature, got %q", branch)
	}
	if commit != "sha1" {
		t.Errorf("expected commit=sha1, got %q", commit)
	}
}

func TestParseGenericWebhookBody_WithBranch(t *testing.T) {
	branch, commit := parseGenericWebhookBody([]byte(`{"branch":"develop"}`))
	if branch != "develop" {
		t.Errorf("expected branch=develop, got %q", branch)
	}
	if commit != "" {
		t.Errorf("expected empty commit, got %q", commit)
	}
}

func TestParseGenericWebhookBody_RefTakesPriority(t *testing.T) {
	branch, _ := parseGenericWebhookBody([]byte(`{"ref":"refs/heads/main","branch":"other"}`))
	if branch != "main" {
		t.Errorf("expected ref to take priority, got branch=%q", branch)
	}
}

// setupEnqueueTest initialises the DB (so db.DB is non-nil) and creates a queue
// without starting workers. Jobs are buffered but never consumed, which prevents
// goroutine races and panics from unrelated DB state in unit tests.
func setupEnqueueTest(t *testing.T) {
	t.Helper()
	setupTestDB(t)
	nop := zerolog.Nop()
	applications.DefaultQueue = applications.NewQueue(&nop)
	t.Cleanup(func() { applications.DefaultQueue = nil })
}

// These tests cover the commit-resolution strategy the generic webhook hands to
// SyncApplications: a supplied commit is used verbatim (no provider call), while an
// omitted commit is resolved once per distinct branch.

func TestGenericWebhookResolve_CommitProvided_SkipsGetLatestCommit(t *testing.T) {
	setupEnqueueTest(t)
	repo := models.Repository{Provider: models.GitHub}
	provider := &mockRepoProvider{latestCommitResult: repositories.CommitInfo{Hash: "latest"}}
	apps := []models.Application{{Branch: "main"}}

	applications.SyncApplications(context.Background(), &repo, provider, apps, applications.StaticCommit("provided-sha", ""), applications.SyncOrigin{Source: models.ApplicationEventSourceRepositoryWebhook}, &applications.Log)

	if len(provider.getLatestCommitCalls) != 0 {
		t.Errorf("expected GetLatestCommit not to be called, got %d calls", len(provider.getLatestCommitCalls))
	}
}

func TestGenericWebhookResolve_NoCommit_FetchesLatestCommitPerBranch(t *testing.T) {
	setupEnqueueTest(t)
	repo := models.Repository{Provider: models.GitHub}
	provider := &mockRepoProvider{latestCommitResult: repositories.CommitInfo{Hash: "resolved"}}
	apps := []models.Application{
		{Branch: "main"},
		{Branch: "develop"},
	}

	applications.SyncApplications(context.Background(), &repo, provider, apps, applications.LatestCommit(provider, &repo), applications.SyncOrigin{Source: models.ApplicationEventSourceRepositoryWebhook}, &applications.Log)

	if len(provider.getLatestCommitCalls) != 2 {
		t.Errorf("expected 2 GetLatestCommit calls, got %d: %v", len(provider.getLatestCommitCalls), provider.getLatestCommitCalls)
	}
}

func TestGenericWebhookResolve_NoCommit_SameBranch_FetchesOnce(t *testing.T) {
	setupEnqueueTest(t)
	repo := models.Repository{Provider: models.GitHub}
	provider := &mockRepoProvider{latestCommitResult: repositories.CommitInfo{Hash: "resolved"}}
	apps := []models.Application{
		{Branch: "main"},
		{Branch: "main"},
		{Branch: "main"},
	}

	applications.SyncApplications(context.Background(), &repo, provider, apps, applications.LatestCommit(provider, &repo), applications.SyncOrigin{Source: models.ApplicationEventSourceRepositoryWebhook}, &applications.Log)

	if len(provider.getLatestCommitCalls) != 1 {
		t.Errorf("expected 1 GetLatestCommit call for same branch, got %d", len(provider.getLatestCommitCalls))
	}
	if provider.getLatestCommitCalls[0] != "main" {
		t.Errorf("expected GetLatestCommit called with branch=main, got %q", provider.getLatestCommitCalls[0])
	}
}
