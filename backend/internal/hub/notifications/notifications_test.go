package notifications

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/notifications/provider"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const validDiscordConfig = `{"token":"token-abc","webhookId":"123456789"}`

func TestMain(m *testing.M) {
	// Tests spin up local httptest servers; swap in an unrestricted client so
	// the SSRF-safe production client does not block 127.0.0.1.
	notificationHTTPClient = http.DefaultClient
	os.Exit(m.Run())
}

func setupNotificationsTestDB(t *testing.T) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	testDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: gormlogger.New(
			log.New(os.Stderr, "\n", log.LstdFlags),
			gormlogger.Config{LogLevel: gormlogger.Warn},
		),
	})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	sqlDB, err := testDB.DB()
	if err != nil {
		t.Fatalf("failed to get sql db: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
		db.DB = nil
	})

	db.DB = testDB

	if err := crypto.Init("test-secret-that-is-long-enough-32chars"); err != nil {
		t.Fatalf("failed to init crypto: %v", err)
	}

	if err := db.DB.AutoMigrate(&models.Repository{}, &models.Agent{}, &models.Application{}, &models.Notification{}); err != nil {
		t.Fatalf("failed to migrate models: %v", err)
	}
}

func seedNotificationTestApp(t *testing.T, healthStatus models.HealthStatus) models.Application {
	t.Helper()

	repo := models.Repository{
		Name:       "Repo",
		Url:        "https://github.com/orcacd/notifications-test-" + uuid.NewString(),
		Provider:   models.GitHub,
		AuthMethod: models.AuthMethodNone,
		SyncType:   models.SyncTypeManual,
		SyncStatus: models.SyncStatusUnknown,
		CreatedBy:  "user-1",
	}
	if err := db.DB.WithContext(t.Context()).Create(&repo).Error; err != nil {
		t.Fatalf("failed to create repository: %v", err)
	}

	agent := models.Agent{
		Name:   crypto.EncryptedString("Agent"),
		KeyId:  crypto.EncryptedString("agent-key"),
		Status: models.AgentStatusOffline,
	}
	if err := db.DB.WithContext(t.Context()).Create(&agent).Error; err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	app := models.Application{
		Name:                crypto.EncryptedString("App"),
		RepositoryId:        repo.Id,
		AgentId:             agent.Id,
		SyncStatus:          models.UnknownSync,
		HealthStatus:        healthStatus,
		Branch:              "main",
		Commit:              "abc123",
		CommitMessage:       "seed",
		Path:                "compose.yaml",
		ComposeFile:         crypto.EncryptedString("services: {}"),
		PreviousComposeFile: crypto.EncryptedString(""),
	}
	if err := db.DB.WithContext(t.Context()).Select("*").Create(&app).Error; err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	return app
}

func seedNotificationRecord(t *testing.T, name string, enabled, allApplications bool, status models.NotificationStatus, appIds ...string) models.Notification {
	t.Helper()

	notification := models.Notification{
		Name:            crypto.EncryptedString(name),
		Enabled:         enabled,
		AllApplications: allApplications,
		Events:          models.NotificationEvents,
		Status:          status,
		Type:            models.NotificationTypeDiscord,
		Config:          crypto.EncryptedString(validDiscordConfig),
	}
	if err := db.DB.WithContext(t.Context()).Select("*").Create(&notification).Error; err != nil {
		t.Fatalf("failed to create notification: %v", err)
	}

	if len(appIds) > 0 {
		applications, err := gorm.G[models.Application](db.DB).Where("id IN ?", appIds).Find(t.Context())
		if err != nil {
			t.Fatalf("failed to load notification applications: %v", err)
		}
		if err := db.DB.Model(&notification).Association("Applications").Replace(applications); err != nil {
			t.Fatalf("failed to associate applications: %v", err)
		}
	}

	return notification
}

func TestGetNotificationConfig_FiltersByStatusAndAssociation(t *testing.T) {
	setupNotificationsTestDB(t)

	app := seedNotificationTestApp(t, models.Healthy)
	otherApp := seedNotificationTestApp(t, models.Unhealthy)

	associated := seedNotificationRecord(t, "associated", true, false, models.NotificationStatusUnknown, app.Id)
	defaultUnknown := seedNotificationRecord(t, "default-unknown", true, true, models.NotificationStatusUnknown)
	withErrorStatus := seedNotificationRecord(t, "with-error-status", true, true, models.NotificationStatusError)
	seedNotificationRecord(t, "disabled", false, true, models.NotificationStatusSuccess)
	seedNotificationRecord(t, "other-app", true, false, models.NotificationStatusSuccess, otherApp.Id)
	otherEvent := seedNotificationRecord(t, "other-event", true, true, models.NotificationStatusUnknown)
	setNotificationEvents(t, otherEvent.Id, models.NotificationEventDeploymentSucceeded)

	configs, err := getNotificationConfig(context.Background(), applicationScope, app.Id, models.NotificationEventDeploymentFailed)
	if err != nil {
		t.Fatalf("getNotificationConfig() error: %v", err)
	}

	ids := make([]string, 0, len(configs))
	for i := range configs {
		ids = append(ids, configs[i].Id)
	}

	if !slices.Contains(ids, associated.Id) {
		t.Fatalf("expected associated notification in result, ids=%v", ids)
	}
	if !slices.Contains(ids, defaultUnknown.Id) {
		t.Fatalf("expected default unknown notification in result, ids=%v", ids)
	}
	if !slices.Contains(ids, withErrorStatus.Id) {
		t.Fatalf("expected default error-status notification in result, ids=%v", ids)
	}
	if len(ids) != 3 {
		t.Fatalf("expected exactly 3 matching notifications, got %d (%v)", len(ids), ids)
	}
}

func associateNotification[T any](t *testing.T, notification *models.Notification, association string, ids ...string) {
	t.Helper()

	records, err := gorm.G[T](db.DB).Where("id IN ?", ids).Find(t.Context())
	if err != nil {
		t.Fatalf("failed to load %s: %v", association, err)
	}
	if err := db.DB.Model(notification).Association(association).Replace(records); err != nil {
		t.Fatalf("failed to associate %s: %v", association, err)
	}
}

func setNotificationEvents(t *testing.T, notificationId string, events ...models.NotificationEvent) {
	t.Helper()

	if _, err := gorm.G[models.Notification](db.DB).
		Where("id = ?", notificationId).
		Select("events").
		Updates(t.Context(), models.Notification{Events: events}); err != nil {
		t.Fatalf("failed to update notification events: %v", err)
	}
}

func notificationIds(configs []models.Notification) []string {
	ids := make([]string, 0, len(configs))
	for i := range configs {
		ids = append(ids, configs[i].Id)
	}
	slices.Sort(ids)
	return ids
}

func TestGetNotificationConfig_ScopesByResourceType(t *testing.T) {
	setupNotificationsTestDB(t)

	app := seedNotificationTestApp(t, models.Healthy)
	otherApp := seedNotificationTestApp(t, models.Healthy)

	allAgents := seedNotificationRecord(t, "all-agents", true, false, models.NotificationStatusUnknown)
	if _, err := gorm.G[models.Notification](db.DB).Where("id = ?", allAgents.Id).Update(t.Context(), "all_agents", true); err != nil {
		t.Fatalf("failed to set all_agents: %v", err)
	}
	allRepositories := seedNotificationRecord(t, "all-repositories", true, false, models.NotificationStatusUnknown)
	if _, err := gorm.G[models.Notification](db.DB).Where("id = ?", allRepositories.Id).Update(t.Context(), "all_repositories", true); err != nil {
		t.Fatalf("failed to set all_repositories: %v", err)
	}

	assigned := seedNotificationRecord(t, "assigned", true, false, models.NotificationStatusUnknown)
	associateNotification[models.Agent](t, &assigned, "Agents", app.AgentId)
	associateNotification[models.Repository](t, &assigned, "Repositories", app.RepositoryId)

	// Covers every application, but neither agents nor repositories.
	seedNotificationRecord(t, "all-applications", true, true, models.NotificationStatusUnknown)

	tests := []struct {
		name  string
		scope notificationScope
		id    string
		event models.NotificationEvent
		want  []string
	}{
		{"assigned agent", agentScope, app.AgentId, models.NotificationEventAgentOffline, []string{allAgents.Id, assigned.Id}},
		{"other agent", agentScope, otherApp.AgentId, models.NotificationEventAgentOffline, []string{allAgents.Id}},
		{"assigned repository", repositoryScope, app.RepositoryId, models.NotificationEventRepositorySyncFailed, []string{allRepositories.Id, assigned.Id}},
		{"other repository", repositoryScope, otherApp.RepositoryId, models.NotificationEventRepositorySyncFailed, []string{allRepositories.Id}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configs, err := getNotificationConfig(t.Context(), tt.scope, tt.id, tt.event)
			if err != nil {
				t.Fatalf("getNotificationConfig() error: %v", err)
			}
			want := slices.Clone(tt.want)
			slices.Sort(want)
			if got := notificationIds(configs); !slices.Equal(got, want) {
				t.Fatalf("expected notifications %v, got %v", want, got)
			}
		})
	}
}

func TestGetNotificationConfig_FiltersAgentEvents(t *testing.T) {
	setupNotificationsTestDB(t)

	app := seedNotificationTestApp(t, models.Healthy)
	offlineOnly := seedNotificationRecord(t, "offline-only", true, false, models.NotificationStatusUnknown)
	setNotificationEvents(t, offlineOnly.Id, models.NotificationEventAgentOffline)
	associateNotification[models.Agent](t, &offlineOnly, "Agents", app.AgentId)

	configs, err := getNotificationConfig(t.Context(), agentScope, app.AgentId, models.NotificationEventAgentOnline)
	if err != nil {
		t.Fatalf("getNotificationConfig() error: %v", err)
	}
	if len(configs) != 0 {
		t.Fatalf("expected no notifications for unsubscribed event, got %v", notificationIds(configs))
	}

	configs, err = getNotificationConfig(t.Context(), agentScope, app.AgentId, models.NotificationEventAgentOffline)
	if err != nil {
		t.Fatalf("getNotificationConfig() error: %v", err)
	}
	if got := notificationIds(configs); !slices.Equal(got, []string{offlineOnly.Id}) {
		t.Fatalf("expected offline-only notification, got %v", got)
	}
}

func TestGetNotificationConfig_ResourceNotFound(t *testing.T) {
	setupNotificationsTestDB(t)

	for _, scope := range []notificationScope{applicationScope, agentScope, repositoryScope} {
		if _, err := getNotificationConfig(t.Context(), scope, "missing", models.NotificationEventAgentOffline); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("%s: expected gorm.ErrRecordNotFound, got %v", scope.logKey, err)
		}
	}
}

func TestBuildShoutrrrUrls_RejectsDirectTargets(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "single raw URL",
			raw:  "discord://token@channel",
		},
		{
			name: "comma separated URLs",
			raw:  "discord://a@1, discord://b@2",
		},
		{
			name: "JSON object with direct URLs",
			raw:  `{"url":"discord://a@1","urls":["discord://b@2"]}`,
		},
		{
			name: "JSON array config",
			raw:  `["discord://a@1","discord://b@2"]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := provider.BuildShoutrrrUrls(models.NotificationTypeDiscord, tt.raw)
			if err == nil {
				t.Fatal("expected BuildShoutrrrUrls() to reject direct targets")
			}
		})
	}
}

func TestBuildShoutrrrUrls_DiscordObjectConfig(t *testing.T) {
	urls, err := provider.BuildShoutrrrUrls(models.NotificationTypeDiscord, `{"token":"token-abc","webhookId":"123456789","threadId":"987654321"}`)
	if err != nil {
		t.Fatalf("BuildShoutrrrUrls() error: %v", err)
	}

	if len(urls) != 1 {
		t.Fatalf("expected 1 URL, got %d", len(urls))
	}
	if !strings.HasPrefix(urls[0], "discord://token-abc@123456789") {
		t.Fatalf("expected discord URL, got %s", urls[0])
	}
	if !strings.Contains(urls[0], "thread_id=987654321") {
		t.Fatalf("expected thread_id in URL, got %s", urls[0])
	}
}

func TestBuildShoutrrrUrls_DiscordObjectConfigMissingFields(t *testing.T) {
	_, err := provider.BuildShoutrrrUrls(models.NotificationTypeDiscord, `{"webhookId":"123456789"}`)
	if err == nil {
		t.Fatal("expected error for missing discord token")
	}
}

func TestGetProvider_Registered(t *testing.T) {
	provider, err := provider.Get(models.NotificationTypeDiscord)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if provider == nil {
		t.Fatal("expected provider to be non-nil")
	}
}

func TestGetProvider_Unregistered(t *testing.T) {
	_, err := provider.Get(models.NotificationType("non-existent"))
	if err == nil {
		t.Fatal("expected error for unregistered provider")
	}
}
