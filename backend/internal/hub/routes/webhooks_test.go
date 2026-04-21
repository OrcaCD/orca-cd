package routes

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func setupTestDBWithWebhookRepos(t *testing.T) {
	t.Helper()
	setupTestDBWithRepos(t)
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
	repoURL := fmt.Sprintf("https://github.com/owner/repo-%s", provider)
	if provider == models.GitLab {
		repoURL = fmt.Sprintf("https://gitlab.com/owner/repo-%s", provider)
	} else if provider == models.Gitea {
		repoURL = fmt.Sprintf("https://gitea.example.com/owner/repo-%s", provider)
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

func makeWebhookRequest(method, repoID, body string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, "/api/v1/webhooks/"+repoID, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: repoID}}
	return c, w
}


func TestWebhookHandler_NotFound(t *testing.T) {
	setupTestDBWithWebhookRepos(t)

	c, w := makeWebhookRequest(http.MethodPost, "nonexistent-id", `{}`)
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

	c, w := makeWebhookRequest(http.MethodPost, repo.Id, `{}`)
	WebhookHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}


func TestWebhookHandler_GitHub_InvalidSignature(t *testing.T) {
	setupTestDBWithWebhookRepos(t)
	repo := seedWebhookRepo(t, models.GitHub, "mysecret")

	c, w := makeWebhookRequest(http.MethodPost, repo.Id, `{"ref":"refs/heads/main"}`)
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

	c, w := makeWebhookRequest(http.MethodPost, repo.Id, `{"ref":"refs/heads/main"}`)
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

	c, w := makeWebhookRequest(http.MethodPost, repo.Id, body)
	c.Request.Header.Set("X-GitHub-Event", "ping")
	c.Request.Header.Set("X-Hub-Signature-256", githubSig(secret, body))
	WebhookHandler(c)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	// Sync status must not have changed
	var got models.Repository
	db.DB.First(&got, "id = ?", repo.Id)
	if got.SyncStatus != models.SyncStatusUnknown {
		t.Errorf("expected sync_status unchanged (%s), got %s", models.SyncStatusUnknown, got.SyncStatus)
	}
}

func TestWebhookHandler_GitHub_PushEvent_UpdatesDB(t *testing.T) {
	setupTestDBWithWebhookRepos(t)
	const secret = "mysecret"
	const body = `{"ref":"refs/heads/main"}`
	repo := seedWebhookRepo(t, models.GitHub, secret)

	c, w := makeWebhookRequest(http.MethodPost, repo.Id, body)
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


func TestWebhookHandler_Gitea_InvalidSignature(t *testing.T) {
	setupTestDBWithWebhookRepos(t)
	repo := seedWebhookRepo(t, models.Gitea, "mysecret")

	c, w := makeWebhookRequest(http.MethodPost, repo.Id, `{"ref":"refs/heads/main"}`)
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
	const body = `{"ref":"refs/heads/main"}`
	repo := seedWebhookRepo(t, models.Gitea, secret)

	c, w := makeWebhookRequest(http.MethodPost, repo.Id, body)
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

	c, w := makeWebhookRequest(http.MethodPost, repo.Id, `{"ref":"refs/heads/main"}`)
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
	const body = `{"ref":"refs/heads/main"}`
	repo := seedWebhookRepo(t, models.GitLab, secret)

	c, w := makeWebhookRequest(http.MethodPost, repo.Id, body)
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

	c, w := makeWebhookRequest(http.MethodPost, repo.Id, `{}`)
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

	c, w := makeWebhookRequest(http.MethodPost, repo.Id, `{}`)
	WebhookHandler(c)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}
