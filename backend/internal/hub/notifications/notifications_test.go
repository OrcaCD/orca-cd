package notifications

import (
	"context"
	"log"
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

func seedNotificationRecord(t *testing.T, name string, enabled, enableByDefault bool, status models.NotificationStatus, appIds ...string) models.Notification {
	t.Helper()

	notification := models.Notification{
		Name:            crypto.EncryptedString(name),
		Enabled:         enabled,
		EnableByDefault: enableByDefault,
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

	configs, err := getNotificationConfig(context.Background(), app.Id)
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
	_, err := provider.Get(models.NotificationType("custom"))
	if err == nil {
		t.Fatal("expected error for unregistered provider")
	}
}
