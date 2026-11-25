package routes

import (
	"bytes"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/composeconfig"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const composeWithConfig = "x-orcacd:\n  icon: rocket\n  delete-old-images: true\n  rollback:\n    enabled: true\n" +
	"services:\n  billing:\n    image: ghcr.io/orcacd/billing:1.0.0\n"

func useComposeFile(t *testing.T, composeFile string) {
	t.Helper()
	t.Cleanup(mockApplicationRepositoryHTTPClient(composeFile))
}

func invokeCreateApplication(t *testing.T, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	reqBody, _ := json.Marshal(body)
	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/applications", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	CreateApplicationHandler(c)
	return w
}

func invokeUpdateApplication(t *testing.T, app models.Application, changes map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]any{
		"name":         app.Name.String(),
		"repositoryId": app.RepositoryId,
		"agentId":      app.AgentId,
		"branch":       app.Branch,
		"path":         app.Path,
	}
	maps.Copy(body, changes)
	reqBody, _ := json.Marshal(body)
	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/applications/"+app.Id, bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: app.Id}}
	UpdateApplicationHandler(c)
	return w
}

// seedManagedApplication seeds an application whose compose file manages the
// settings of composeWithConfig.
func seedManagedApplication(t *testing.T) models.Application {
	t.Helper()
	repo := seedTestRepository(t, "https://github.com/owner/repo-compose-config")
	agent := seedTestAgent(t, "agent-compose-config")
	app := seedTestApplication(t, repo.Id, agent.Id, "Managed App")
	cfg, err := composeconfig.Parse(composeWithConfig)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Apply(&app)
	if err := db.DB.Save(&app).Error; err != nil {
		t.Fatalf("save application: %v", err)
	}
	return app
}

func TestCreateApplicationHandler_AppliesComposeConfig(t *testing.T) {
	setupTestDBWithApplications(t)
	useComposeFile(t, composeWithConfig)
	repo := seedTestRepository(t, "https://github.com/owner/repo-create-config")
	agent := seedTestAgent(t, "agent-create-config")

	w := invokeCreateApplication(t, validApplicationRequestBody(repo.Id, agent.Id))
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var body applicationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Icon != "rocket" || !body.DeleteOldImages || !body.Rollback.Enabled {
		t.Errorf("expected the settings of the compose file, got icon %q, deleteOldImages %v, rollback %+v", body.Icon, body.DeleteOldImages, body.Rollback)
	}
	want := []string{composeconfig.SettingIcon, composeconfig.SettingDeleteOldImages, composeconfig.SettingRollbackEnabled}
	if !slices.Equal(body.ManagedSettings, want) {
		t.Errorf("managedSettings = %v, want %v", body.ManagedSettings, want)
	}
}

func TestCreateApplicationHandler_InvalidComposeConfig(t *testing.T) {
	setupTestDBWithApplications(t)
	useComposeFile(t, "x-orcacd:\n  sync: auto\nservices: {}\n")
	repo := seedTestRepository(t, "https://github.com/owner/repo-create-invalid-config")
	agent := seedTestAgent(t, "agent-create-invalid-config")

	w := invokeCreateApplication(t, validApplicationRequestBody(repo.Id, agent.Id))
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), `x-orcacd: unknown key \"sync\"`) {
		t.Fatalf("expected 422 naming the unknown key, got %d: %s", w.Code, w.Body.String())
	}
	if count, _ := gorm.G[models.Application](db.DB).Count(t.Context(), "*"); count != 0 {
		t.Errorf("expected no application to be created, got %d", count)
	}
}

func TestGetApplicationHandler_ManagedSettingsDefaultsToEmpty(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-managed-default")
	agent := seedTestAgent(t, "agent-managed-default")
	app := seedTestApplication(t, repo.Id, agent.Id, "Plain App")

	response := toApplicationResponse(&app)
	if response.ManagedSettings == nil || len(response.ManagedSettings) != 0 {
		t.Errorf("managedSettings = %v, want []", response.ManagedSettings)
	}
}

func TestUpdateApplicationHandler_ComposeConfig(t *testing.T) {
	setupTestDBWithApplications(t)
	useComposeFile(t, composeWithConfig)
	app := seedManagedApplication(t)

	w := invokeUpdateApplication(t, app, map[string]any{"deleteOldImages": false})
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), composeconfig.SettingDeleteOldImages) {
		t.Fatalf("expected 409 naming deleteOldImages, got %d: %s", w.Code, w.Body.String())
	}

	// Unmanaged settings can change, managed ones keep their value.
	w = invokeUpdateApplication(t, app, map[string]any{
		"icon": "rocket", "deleteOldImages": true,
		"imagePollEnabled": true, "imagePollIntervalSeconds": 300,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !updated.ImagePollEnabled || updated.ImagePollIntervalSeconds != 300 ||
		updated.Icon != "rocket" || !updated.DeleteOldImages || !updated.RollbackEnabled {
		t.Errorf("unexpected application after update: %+v", updated)
	}
}

func TestUpdateApplicationHandler_AppliesNewComposeConfig(t *testing.T) {
	setupTestDBWithApplications(t)
	app := seedManagedApplication(t)
	// The compose file at the latest commit no longer manages the icon and
	// rollback, but enables image polling.
	useComposeFile(t, "x-orcacd:\n  delete-old-images: true\n  image-poll:\n    enabled: true\n    interval: 10m\nservices: {}\n")

	w := invokeUpdateApplication(t, app, map[string]any{"deleteOldImages": true})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !updated.ImagePollEnabled || updated.ImagePollIntervalSeconds != 600 || updated.RollbackEnabled || updated.Icon != "box" {
		t.Errorf("expected the settings of the new compose file, got %+v", updated)
	}
	want := []string{composeconfig.SettingImagePollEnabled, composeconfig.SettingImagePollIntervalSeconds, composeconfig.SettingDeleteOldImages}
	if !slices.Equal(updated.ManagedSettings, want) {
		t.Errorf("ManagedSettings = %v, want %v", updated.ManagedSettings, want)
	}
}

func TestUpdateRollbackSettingsHandler_ManagedSettings(t *testing.T) {
	setupTestDBWithApplications(t)
	app := seedManagedApplication(t)

	w := invokeRollbackSettingsHandler(t, app.Id, `{"enabled":false,"onDeployFailure":true,"onUnhealthy":true,"healthTimeoutSeconds":300,"pauseAutoSync":true}`)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), composeconfig.SettingRollbackEnabled) {
		t.Fatalf("expected 409 naming rollback.enabled, got %d: %s", w.Code, w.Body.String())
	}

	response := decodeApplication(t, invokeRollbackSettingsHandler(t, app.Id, `{"enabled":true,"onDeployFailure":false,"onUnhealthy":true,"healthTimeoutSeconds":600,"pauseAutoSync":true}`))
	if !response.Rollback.Enabled || response.Rollback.OnDeployFailure || response.Rollback.HealthTimeoutSeconds != 600 {
		t.Errorf("expected the unmanaged rollback settings to change, got %+v", response.Rollback)
	}
}

func TestUpdateReleaseAgeSettingsHandler_ManagedSettings(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-release-age-config")
	agent := seedTestAgent(t, "agent-release-age-config")
	app := seedTestApplication(t, repo.Id, agent.Id, "Release Age Config App")
	cfg, err := composeconfig.Parse("x-orcacd:\n  release-age:\n    allowed-registries: [ghcr.io]\n")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Apply(&app)
	if err := db.DB.Save(&app).Error; err != nil {
		t.Fatal(err)
	}

	w := invokeReleaseAgeSettingsHandler(t, app.Id, `{"minimumSeconds":3600,"allowedRegistries":[]}`)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), composeconfig.SettingReleaseAgeAllowedRegistries) {
		t.Fatalf("expected 409 naming releaseAge.allowedRegistries, got %d: %s", w.Code, w.Body.String())
	}

	response := decodeApplication(t, invokeReleaseAgeSettingsHandler(t, app.Id, `{"minimumSeconds":3600,"allowedRegistries":["GHCR.io"]}`))
	if response.ReleaseAge.MinimumSeconds == nil || *response.ReleaseAge.MinimumSeconds != 3600 {
		t.Errorf("expected the unmanaged minimum to change, got %+v", response.ReleaseAge)
	}
}

func TestUpdateReleaseAgeSettingsHandler_ManagedAgeBelowGlobalMinimum(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-release-age-below")
	agent := seedTestAgent(t, "agent-release-age-below")
	app := seedTestApplication(t, repo.Id, agent.Id, "Release Age Below App")
	cfg, err := composeconfig.Parse("x-orcacd:\n  release-age:\n    minimum: 1m\n")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Apply(&app)
	if err := db.DB.Save(&app).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Save(&models.Settings{Id: models.SettingsId, MinimumReleaseAgeSeconds: 86400, ReleaseAgeAllowedRegistries: []string{}}).Error; err != nil {
		t.Fatal(err)
	}

	// The age from the compose file is below the global minimum, which then
	// applies, so saving the other settings must still work.
	response := decodeApplication(t, invokeReleaseAgeSettingsHandler(t, app.Id, `{"minimumSeconds":60,"allowedRegistries":["ghcr.io"]}`))
	if !slices.Equal(response.ReleaseAge.AllowedRegistries, []string{"ghcr.io"}) {
		t.Errorf("allowedRegistries = %v, want [ghcr.io]", response.ReleaseAge.AllowedRegistries)
	}
}

// seedNotificationManagedApplication seeds an application that sets its
// notifications with x-orcacd, assigned to notification.
func seedNotificationManagedApplication(t *testing.T, name string) models.Application {
	t.Helper()
	app := seedNotificationApplication(t, seedNotificationRepository(t).Id, seedNotificationAgent(t).Id, name)
	app.ManagedSettings = []string{composeconfig.SettingNotifications}
	if err := db.DB.Model(&app).Select("ManagedSettings").Updates(&app).Error; err != nil {
		t.Fatal(err)
	}
	return app
}

func invokeNotificationHandler(t *testing.T, method, id string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	reqBody, _ := json.Marshal(body)
	router := newAdminRouter()
	router.POST("/api/v1/notifications", CreateNotificationHandler)
	router.PUT("/api/v1/notifications/:id", UpdateNotificationHandler)
	path := "/api/v1/notifications"
	if id != "" {
		path += "/" + id
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	return w
}

func notificationApplicationIds(t *testing.T, id string) []string {
	t.Helper()
	stored, err := gorm.G[models.Notification](db.DB).Preload("Applications", nil).Where("id = ?", id).First(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, app := range stored.Applications {
		ids = append(ids, app.Id)
	}
	return ids
}

func TestUpdateNotificationHandler_ManagedApplications(t *testing.T) {
	setupTestDBWithNotifications(t)
	managed := seedNotificationManagedApplication(t, "Managed App")
	plain := seedNotificationApplication(t, managed.RepositoryId, managed.AgentId, "Plain App")
	notification := createNotificationRecord(t, []string{managed.Id})
	if err := db.DB.Model(&notification).Update("compose_assignable", true).Error; err != nil {
		t.Fatal(err)
	}
	update := func(allApplications bool, applicationIds []string) *httptest.ResponseRecorder {
		return invokeNotificationHandler(t, http.MethodPut, notification.Id, map[string]any{
			"enabled": true, "allApplications": allApplications, "allAgents": false, "allRepositories": false,
			"events": []string{string(models.NotificationEventDeploymentFailed)}, "applicationIds": applicationIds,
		})
	}

	if w := update(false, []string{plain.Id}); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "Managed App") {
		t.Fatalf("expected 409 for removing the managed application, got %d: %s", w.Code, w.Body.String())
	}
	if w := update(false, []string{managed.Id, plain.Id}); w.Code != http.StatusOK {
		t.Fatalf("expected 200 when keeping the managed application, got %d: %s", w.Code, w.Body.String())
	}
	// Telling about all applications keeps the link from x-orcacd.
	if w := update(true, nil); w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ids := notificationApplicationIds(t, notification.Id); !slices.Equal(ids, []string{managed.Id}) {
		t.Errorf("applications = %v, want only the managed one", ids)
	}
}

func TestCreateNotificationHandler_ManagedApplication(t *testing.T) {
	setupTestDBWithNotifications(t)
	managed := seedNotificationManagedApplication(t, "Managed App")

	create := func(composeAssignable bool) *httptest.ResponseRecorder {
		return invokeNotificationHandler(t, http.MethodPost, "", map[string]any{
			"name": "Ops", "type": "discord", "config": json.RawMessage(validDiscordConfig),
			"applicationIds": []string{managed.Id}, "composeAssignable": composeAssignable,
		})
	}
	if w := create(true); w.Code != http.StatusConflict {
		t.Fatalf("expected 409 for assigning the managed application, got %d: %s", w.Code, w.Body.String())
	}
	// Notifications that compose files can't assign are not locked.
	if w := create(false); w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateNotificationHandler_ComposeAssignable(t *testing.T) {
	setupTestDBWithNotifications(t)
	app := seedNotificationApplication(t, seedNotificationRepository(t).Id, seedNotificationAgent(t).Id, "App")
	notification := createNotificationRecord(t, []string{app.Id})
	update := func(body map[string]any) notificationResponse {
		t.Helper()
		w := invokeNotificationHandler(t, http.MethodPut, notification.Id, body)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var response notificationResponse
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		return response
	}
	body := map[string]any{
		"enabled": true, "allApplications": false, "allAgents": false, "allRepositories": false,
		"events": []string{string(models.NotificationEventDeploymentFailed)}, "applicationIds": []string{app.Id},
	}

	body["composeAssignable"] = true
	if response := update(body); !response.ComposeAssignable {
		t.Error("expected composeAssignable to be set")
	}
	// Leaving it out keeps it.
	delete(body, "composeAssignable")
	if response := update(body); !response.ComposeAssignable {
		t.Error("expected composeAssignable to be kept")
	}
}

func TestCreateApplicationHandler_AssignsNotifications(t *testing.T) {
	setupTestDBWithApplications(t)
	if err := db.DB.AutoMigrate(&models.Notification{}); err != nil {
		t.Fatal(err)
	}
	notification := createNotificationRecord(t, nil)
	if err := db.DB.Model(&notification).Update("compose_assignable", true).Error; err != nil {
		t.Fatal(err)
	}
	useComposeFile(t, "x-orcacd:\n  notifications: [Initial Notification]\nservices: {}\n")
	repo := seedTestRepository(t, "https://github.com/owner/repo-create-notifications")
	agent := seedTestAgent(t, "agent-create-notifications")

	w := invokeCreateApplication(t, validApplicationRequestBody(repo.Id, agent.Id))
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var body applicationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if ids := notificationApplicationIds(t, notification.Id); !slices.Equal(ids, []string{body.Id}) {
		t.Errorf("notification applications = %v, want [%s]", ids, body.Id)
	}

	useComposeFile(t, "x-orcacd:\n  notifications: [Unknown]\nservices: {}\n")
	w = invokeCreateApplication(t, map[string]any{
		"name": "Other", "repositoryId": repo.Id, "agentId": agent.Id, "branch": "main", "path": "services/billing.yml",
	})
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), `no notification \"Unknown\" allows compose files`) {
		t.Fatalf("expected 422 for an unknown notification, got %d: %s", w.Code, w.Body.String())
	}
}
