package routes

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/applications"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	hubws "github.com/OrcaCD/orca-cd/internal/hub/websocket"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

func setupTestDBForImagePullWebhook(t *testing.T) {
	t.Helper()
	setupTestDB(t)
	if err := db.DB.AutoMigrate(&models.Agent{}, &models.Repository{}, &models.Application{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	nop := zerolog.Nop()
	applications.DefaultQueue = applications.NewQueue(&nop)
	applications.DefaultQueue.Start()
	t.Cleanup(func() { applications.DefaultQueue = nil })
}

func seedAppWithWebhookSecret(t *testing.T, secret string) models.Application {
	t.Helper()

	// Seed a minimal agent and repository first (Application has FK constraints)
	agent := models.Agent{
		Name: crypto.EncryptedString("test-agent"),
	}
	if err := db.DB.Select("*").Create(&agent).Error; err != nil {
		t.Fatalf("failed to seed agent: %v", err)
	}

	repo := models.Repository{
		Name:       "owner/repo",
		Url:        "https://github.com/owner/repo-pull-webhook",
		Provider:   models.GitHub,
		AuthMethod: models.AuthMethodNone,
		SyncType:   models.SyncTypeManual,
		SyncStatus: models.SyncStatusUnknown,
		CreatedBy:  "user-1",
	}
	if err := db.DB.Select("*").Create(&repo).Error; err != nil {
		t.Fatalf("failed to seed repository: %v", err)
	}

	var enc *crypto.EncryptedString
	if secret != "" {
		e := crypto.EncryptedString(secret)
		enc = &e
	}

	app := models.Application{
		Name:               crypto.EncryptedString("test-app"),
		RepositoryId:       repo.Id,
		AgentId:            agent.Id,
		SyncStatus:         models.UnknownSync,
		HealthStatus:       models.UnknownHealth,
		Branch:             "main",
		Path:               "docker-compose.yml",
		ComposeFile:        crypto.EncryptedString("version: '3'\n"),
		ImageWebhookSecret: enc,
	}
	if err := db.DB.Select("*").Create(&app).Error; err != nil {
		t.Fatalf("failed to seed app: %v", err)
	}
	return app
}

func makeImagePullWebhookRequest(appID, bearerToken string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/images/"+appID, strings.NewReader("{}"))
	c.Request.Header.Set("Content-Type", "application/json")
	if bearerToken != "" {
		c.Request.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	c.Params = gin.Params{{Key: "id", Value: appID}}
	return c, w
}

func TestImagePullWebhookHandler_NotFound(t *testing.T) {
	setupTestDBForImagePullWebhook(t)

	c, w := makeImagePullWebhookRequest("nonexistent-id", "sometoken")
	ImagePullWebhookHandler(c)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestImagePullWebhookHandler_WebhookNotConfigured(t *testing.T) {
	setupTestDBForImagePullWebhook(t)
	app := seedAppWithWebhookSecret(t, "") // no secret

	c, w := makeImagePullWebhookRequest(app.Id, "sometoken")
	ImagePullWebhookHandler(c)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 when webhook not configured, got %d: %s", w.Code, w.Body.String())
	}
}

func TestImagePullWebhookHandler_MissingAuth(t *testing.T) {
	setupTestDBForImagePullWebhook(t)
	app := seedAppWithWebhookSecret(t, "mysecret")

	c, w := makeImagePullWebhookRequest(app.Id, "") // no token
	ImagePullWebhookHandler(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestImagePullWebhookHandler_WrongToken(t *testing.T) {
	setupTestDBForImagePullWebhook(t)
	app := seedAppWithWebhookSecret(t, "correctsecret")

	c, w := makeImagePullWebhookRequest(app.Id, "wrongsecret")
	ImagePullWebhookHandler(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestImagePullWebhookHandler_ValidToken_SendsPullImagesRequest(t *testing.T) {
	setupTestDBForImagePullWebhook(t)

	const secret = "mysecret"
	const agentID = "agent-pull-webhook-test"

	nop := zerolog.Nop()
	hub := hubws.NewHub(&nop)
	hubws.DefaultHub = hub
	t.Cleanup(func() { hubws.DefaultHub = nil })

	app := seedAppWithWebhookSecret(t, secret)

	// Override the agent ID so the hub can find it
	if err := db.DB.Model(&models.Application{}).Where("id = ?", app.Id).Update("agent_id", agentID).Error; err != nil {
		t.Fatalf("failed to update agent_id: %v", err)
	}

	client, err := hub.Register(agentID, nil)
	if err != nil {
		t.Fatalf("failed to register agent: %v", err)
	}

	c, w := makeImagePullWebhookRequest(app.Id, secret)
	ImagePullWebhookHandler(c)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	select {
	case msg := <-client.Send:
		req := msg.GetPullImagesRequest()
		if req == nil {
			t.Fatalf("expected PullImagesRequest, got %T", msg.Payload)
		}
		if req.ApplicationId != app.Id {
			t.Errorf("application_id: got %q, want %q", req.ApplicationId, app.Id)
		}
		if req.RequestId == "" {
			t.Error("expected non-empty request_id")
		}
	default:
		t.Error("expected a PullImagesRequest to be queued on the agent's Send channel")
	}
}

func TestImagePullWebhookHandler_ValidToken_AgentDisconnected_Returns204(t *testing.T) {
	setupTestDBForImagePullWebhook(t)

	const secret = "mysecret"

	nop := zerolog.Nop()
	hub := hubws.NewHub(&nop)
	hubws.DefaultHub = hub
	t.Cleanup(func() { hubws.DefaultHub = nil })

	app := seedAppWithWebhookSecret(t, secret)
	// Agent is not registered in hub — TriggerImagePull will silently return false

	c, w := makeImagePullWebhookRequest(app.Id, secret)
	ImagePullWebhookHandler(c)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204 even when agent is offline, got %d: %s", w.Code, w.Body.String())
	}
}

// Verify that the secret is compared in constant time: an empty token must not match.
func TestImagePullWebhookHandler_EmptyToken_Rejected(t *testing.T) {
	setupTestDBForImagePullWebhook(t)
	app := seedAppWithWebhookSecret(t, "")

	// Force a secret into the DB directly to have a configured webhook
	enc := crypto.EncryptedString("realsecret")
	if err := db.DB.Model(&models.Application{}).Where("id = ?", app.Id).Update("image_webhook_secret", &enc).Error; err != nil {
		t.Fatalf("failed to set secret: %v", err)
	}

	// Send request with no Authorization header (empty token extracted from "Bearer ")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/images/"+app.Id, nil)
	c.Request.Header.Set("Authorization", "Bearer ")
	c.Params = gin.Params{{Key: "id", Value: app.Id}}
	ImagePullWebhookHandler(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for empty bearer token, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGenerateImagePullWebhookHandler_NotFound(t *testing.T) {
	setupTestDBForImagePullWebhook(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/applications/nonexistent/image-webhook", nil)
	c.Params = gin.Params{{Key: "id", Value: "nonexistent"}}
	GenerateImagePullWebhookHandler(c)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGenerateImagePullWebhookHandler_CreatesSecret(t *testing.T) {
	setupTestDBForImagePullWebhook(t)
	app := seedAppWithWebhookSecret(t, "")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/applications/"+app.Id+"/image-webhook", nil)
	c.Params = gin.Params{{Key: "id", Value: app.Id}}
	GenerateImagePullWebhookHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp generateImagePullWebhookResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Secret == "" {
		t.Error("expected non-empty secret in response")
	}
	if resp.WebhookUrl == "" {
		t.Error("expected non-empty webhook URL in response")
	}

	// Verify the secret was persisted in DB
	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(c.Request.Context())
	if err != nil {
		t.Fatalf("failed to reload app: %v", err)
	}
	if updated.ImageWebhookSecret == nil {
		t.Error("expected ImageWebhookSecret to be set in DB")
	}
	if updated.ImageWebhookSecret.String() != resp.Secret {
		t.Error("DB secret does not match returned secret")
	}
}

func TestRevokeImagePullWebhookHandler_NotFound(t *testing.T) {
	setupTestDBForImagePullWebhook(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/applications/nonexistent/image-webhook", nil)
	c.Params = gin.Params{{Key: "id", Value: "nonexistent"}}
	RevokeImagePullWebhookHandler(c)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRevokeImagePullWebhookHandler_ClearsSecret(t *testing.T) {
	setupTestDBForImagePullWebhook(t)
	app := seedAppWithWebhookSecret(t, "existingsecret")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/applications/"+app.Id+"/image-webhook", nil)
	c.Params = gin.Params{{Key: "id", Value: app.Id}}
	RevokeImagePullWebhookHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify the secret was cleared in DB
	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(c.Request.Context())
	if err != nil {
		t.Fatalf("failed to reload app: %v", err)
	}
	if updated.ImageWebhookSecret != nil {
		t.Error("expected ImageWebhookSecret to be nil after revoke")
	}
}

// setupHubForTest creates a Hub, registers it as the default, and cleans up on test completion.
func setupHubForTest(t *testing.T) *hubws.Hub {
	t.Helper()
	log := zerolog.Nop()
	hub := hubws.NewHub(&log)
	hubws.DefaultHub = hub
	t.Cleanup(func() { hubws.DefaultHub = nil })
	return hub
}

func makeGitHubPackageRequest(appID, body, sig string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/images/"+appID, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("X-GitHub-Event", "package")
	c.Request.Header.Set("X-Hub-Signature-256", sig)
	c.Params = gin.Params{{Key: "id", Value: appID}}
	return c, w
}

func imagePullHMAC(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestImagePullWebhookHandler_GitHubPackage_Published_TriggersPull(t *testing.T) {
	setupTestDBForImagePullWebhook(t)
	hub := setupHubForTest(t)

	const secret = "mysecret"
	const agentID = "agent-pkg-published"
	const body = `{"action":"published","package":{"package_type":"CONTAINER"}}`

	app := seedAppWithWebhookSecret(t, secret)
	if err := db.DB.Model(&models.Application{}).Where("id = ?", app.Id).Update("agent_id", agentID).Error; err != nil {
		t.Fatalf("failed to update agent_id: %v", err)
	}

	client, err := hub.Register(agentID, nil)
	if err != nil {
		t.Fatalf("failed to register agent: %v", err)
	}

	c, w := makeGitHubPackageRequest(app.Id, body, imagePullHMAC(secret, body))
	ImagePullWebhookHandler(c)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	select {
	case msg := <-client.Send:
		req := msg.GetPullImagesRequest()
		if req == nil {
			t.Fatalf("expected PullImagesRequest, got %T", msg.Payload)
		}
		if req.ApplicationId != app.Id {
			t.Errorf("application_id: got %q, want %q", req.ApplicationId, app.Id)
		}
		if req.RequestId == "" {
			t.Error("expected non-empty request_id")
		}
	default:
		t.Error("expected a PullImagesRequest to be queued on the agent's Send channel")
	}
}

func TestImagePullWebhookHandler_GitHubPackage_Updated_TriggersPull(t *testing.T) {
	setupTestDBForImagePullWebhook(t)
	hub := setupHubForTest(t)

	const secret = "mysecret"
	const agentID = "agent-pkg-updated"
	const body = `{"action":"updated","package":{"package_type":"CONTAINER"}}`

	app := seedAppWithWebhookSecret(t, secret)
	if err := db.DB.Model(&models.Application{}).Where("id = ?", app.Id).Update("agent_id", agentID).Error; err != nil {
		t.Fatalf("failed to update agent_id: %v", err)
	}

	client, err := hub.Register(agentID, nil)
	if err != nil {
		t.Fatalf("failed to register agent: %v", err)
	}

	c, w := makeGitHubPackageRequest(app.Id, body, imagePullHMAC(secret, body))
	ImagePullWebhookHandler(c)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	select {
	case msg := <-client.Send:
		if msg.GetPullImagesRequest() == nil {
			t.Errorf("expected PullImagesRequest payload, got %T", msg.Payload)
		}
	default:
		t.Error("expected a PullImagesRequest to be queued on the agent's Send channel")
	}
}

func TestImagePullWebhookHandler_GitHubPackage_InvalidSignature_Returns401(t *testing.T) {
	setupTestDBForImagePullWebhook(t)

	const body = `{"action":"published","package":{"package_type":"CONTAINER"}}`
	app := seedAppWithWebhookSecret(t, "mysecret")

	c, w := makeGitHubPackageRequest(app.Id, body, imagePullHMAC("wrongsecret", body))
	ImagePullWebhookHandler(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestImagePullWebhookHandler_GitHubPackage_NonContainer_Returns204NoPull(t *testing.T) {
	setupTestDBForImagePullWebhook(t)
	setupHubForTest(t)

	const secret = "mysecret"
	const body = `{"action":"published","package":{"package_type":"NPM"}}`
	app := seedAppWithWebhookSecret(t, secret)

	c, w := makeGitHubPackageRequest(app.Id, body, imagePullHMAC(secret, body))
	ImagePullWebhookHandler(c)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestImagePullWebhookHandler_GitHubPackage_NonPublishedAction_Returns204NoPull(t *testing.T) {
	setupTestDBForImagePullWebhook(t)
	setupHubForTest(t)

	const secret = "mysecret"
	const body = `{"action":"deleted","package":{"package_type":"CONTAINER"}}`
	app := seedAppWithWebhookSecret(t, secret)

	c, w := makeGitHubPackageRequest(app.Id, body, imagePullHMAC(secret, body))
	ImagePullWebhookHandler(c)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

// --- Harbor webhook tests ---

func makeHarborWebhookRequest(appID, bearerToken, body string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/images/"+appID, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	if bearerToken != "" {
		c.Request.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	c.Params = gin.Params{{Key: "id", Value: appID}}
	return c, w
}

func TestImagePullWebhookHandler_Harbor_PushImage_TriggersPull(t *testing.T) {
	setupTestDBForImagePullWebhook(t)
	hub := setupHubForTest(t)

	const secret = "harborsecret"
	const agentID = "agent-harbor-push"
	const body = `{"event_type":"pushImage","event_data":{"repository":{"name":"myimage"}}}`

	app := seedAppWithWebhookSecret(t, secret)
	if err := db.DB.Model(&models.Application{}).Where("id = ?", app.Id).Update("agent_id", agentID).Error; err != nil {
		t.Fatalf("failed to update agent_id: %v", err)
	}

	client, err := hub.Register(agentID, nil)
	if err != nil {
		t.Fatalf("failed to register agent: %v", err)
	}

	c, w := makeHarborWebhookRequest(app.Id, secret, body)
	ImagePullWebhookHandler(c)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	select {
	case msg := <-client.Send:
		req := msg.GetPullImagesRequest()
		if req == nil {
			t.Fatalf("expected PullImagesRequest, got %T", msg.Payload)
		}
		if req.ApplicationId != app.Id {
			t.Errorf("application_id: got %q, want %q", req.ApplicationId, app.Id)
		}
	default:
		t.Error("expected a PullImagesRequest to be queued on the agent's Send channel")
	}
}

func TestImagePullWebhookHandler_Harbor_DeleteImage_Returns204NoPull(t *testing.T) {
	setupTestDBForImagePullWebhook(t)
	setupHubForTest(t)

	const secret = "harborsecret"
	const body = `{"event_type":"deleteImage"}`
	app := seedAppWithWebhookSecret(t, secret)

	c, w := makeHarborWebhookRequest(app.Id, secret, body)
	ImagePullWebhookHandler(c)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestImagePullWebhookHandler_Harbor_PullImage_Returns204NoPull(t *testing.T) {
	setupTestDBForImagePullWebhook(t)
	setupHubForTest(t)

	const secret = "harborsecret"
	const body = `{"event_type":"pullImage"}`
	app := seedAppWithWebhookSecret(t, secret)

	c, w := makeHarborWebhookRequest(app.Id, secret, body)
	ImagePullWebhookHandler(c)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestImagePullWebhookHandler_NoEventType_TriggersPull(t *testing.T) {
	setupTestDBForImagePullWebhook(t)
	hub := setupHubForTest(t)

	const secret = "mysecret"
	const agentID = "agent-generic-webhook"

	app := seedAppWithWebhookSecret(t, secret)
	if err := db.DB.Model(&models.Application{}).Where("id = ?", app.Id).Update("agent_id", agentID).Error; err != nil {
		t.Fatalf("failed to update agent_id: %v", err)
	}

	client, err := hub.Register(agentID, nil)
	if err != nil {
		t.Fatalf("failed to register agent: %v", err)
	}

	// Body with no event_type should always trigger (generic webhook)
	c, w := makeHarborWebhookRequest(app.Id, secret, `{"some":"data"}`)
	ImagePullWebhookHandler(c)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	select {
	case msg := <-client.Send:
		if msg.GetPullImagesRequest() == nil {
			t.Errorf("expected PullImagesRequest payload, got %T", msg.Payload)
		}
	default:
		t.Error("expected a PullImagesRequest to be queued on the agent's Send channel")
	}
}

func TestImagePullWebhookHandler_DBError(t *testing.T) {
	setupTestDBForImagePullWebhook(t)
	closeDBForErrorPath(t)

	c, w := makeImagePullWebhookRequest("any-id", "token")
	ImagePullWebhookHandler(c)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestImagePullWebhookHandler_GitHubPackage_InvalidJSON_Returns400(t *testing.T) {
	setupTestDBForImagePullWebhook(t)

	const secret = "mysecret"
	const body = "not-valid-json"
	app := seedAppWithWebhookSecret(t, secret)

	c, w := makeGitHubPackageRequest(app.Id, body, imagePullHMAC(secret, body))
	ImagePullWebhookHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGenerateImagePullWebhookHandler_DBError(t *testing.T) {
	setupTestDBForImagePullWebhook(t)
	closeDBForErrorPath(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/applications/any-id/image-webhook", nil)
	c.Params = gin.Params{{Key: "id", Value: "any-id"}}
	GenerateImagePullWebhookHandler(c)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRevokeImagePullWebhookHandler_DBError(t *testing.T) {
	setupTestDBForImagePullWebhook(t)
	closeDBForErrorPath(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/applications/any-id/image-webhook", nil)
	c.Params = gin.Params{{Key: "id", Value: "any-id"}}
	RevokeImagePullWebhookHandler(c)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestIsHarborPushEvent(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"pushImage triggers", `{"event_type":"pushImage"}`, true},
		{"deleteImage ignored", `{"event_type":"deleteImage"}`, false},
		{"pullImage ignored", `{"event_type":"pullImage"}`, false},
		{"scanningCompleted ignored", `{"event_type":"scanningCompleted"}`, false},
		{"no event_type triggers", `{"foo":"bar"}`, true},
		{"empty body triggers", ``, true},
		{"invalid json triggers", `not json`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isHarborPushEvent([]byte(tt.body))
			if got != tt.want {
				t.Errorf("isHarborPushEvent(%q) = %v, want %v", tt.body, got, tt.want)
			}
		})
	}
}
