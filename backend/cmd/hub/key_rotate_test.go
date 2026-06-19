package main

import (
	"path/filepath"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const (
	testOldAppSecret = "old-secret-that-is-long-enough-32chars"
	testNewAppSecret = "new-secret-that-is-long-enough-32chars"
)

type keyRotateFixture struct {
	AgentID        string
	ApplicationID  string
	RepositoryID   string
	NotificationID string
	OIDCProviderID string
}

func setupKeyRotateTestDB(t *testing.T) {
	t.Helper()

	testDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	if err := testDB.AutoMigrate(
		&models.Agent{},
		&models.Repository{},
		&models.Application{},
		&models.Notification{},
		&models.OIDCProvider{},
	); err != nil {
		t.Fatalf("failed to migrate test db: %v", err)
	}

	sqlDB, err := testDB.DB()
	if err != nil {
		t.Fatalf("failed to access sql db: %v", err)
	}

	db.DB = testDB
	t.Cleanup(func() {
		_ = sqlDB.Close()
		db.DB = nil
	})
}

func seedKeyRotateFixture(t *testing.T) keyRotateFixture {
	t.Helper()

	if err := crypto.Init(testOldAppSecret); err != nil {
		t.Fatalf("failed to init old crypto: %v", err)
	}

	agent := models.Agent{
		Name:   crypto.EncryptedString("agent-name"),
		Icon:   "server",
		KeyId:  crypto.EncryptedString("agent-key-id"),
		Status: models.AgentStatusOffline,
	}
	if err := gorm.G[models.Agent](db.DB).Create(t.Context(), &agent); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	authUser := crypto.EncryptedString("repo-user")
	authToken := crypto.EncryptedString("repo-token")
	webhookSecret := crypto.EncryptedString("repo-webhook-secret")
	repository := models.Repository{
		Name:          "repository-name",
		Url:           "https://example.com/org/repo.git",
		Provider:      models.GitHub,
		AuthMethod:    models.AuthMethodBasic,
		AuthUser:      &authUser,
		AuthToken:     &authToken,
		SyncType:      models.SyncTypeWebhook,
		SyncStatus:    models.SyncStatusSuccess,
		WebhookSecret: &webhookSecret,
		CreatedBy:     "user-id",
	}
	if err := gorm.G[models.Repository](db.DB).Create(t.Context(), &repository); err != nil {
		t.Fatalf("failed to create repository: %v", err)
	}

	imageWebhookSecret := crypto.EncryptedString("image-webhook-secret")
	application := models.Application{
		Name:                crypto.EncryptedString("application-name"),
		Icon:                "box",
		RepositoryId:        repository.Id,
		AgentId:             agent.Id,
		SyncStatus:          models.Synced,
		HealthStatus:        models.Healthy,
		Branch:              "main",
		Commit:              "abc123",
		CommitMessage:       "initial commit",
		Path:                "compose.yaml",
		ComposeFile:         crypto.EncryptedString("services:\n  app:\n    image: nginx\n"),
		PreviousComposeFile: crypto.EncryptedString("services: {}\n"),
		ImageWebhookSecret:  &imageWebhookSecret,
	}
	if err := gorm.G[models.Application](db.DB).Create(t.Context(), &application); err != nil {
		t.Fatalf("failed to create application: %v", err)
	}

	notification := models.Notification{
		Name:            crypto.EncryptedString("notification-name"),
		Enabled:         true,
		EnableByDefault: true,
		Status:          models.NotificationStatusUnknown,
		Type:            models.NotificationTypeDiscord,
		Config:          crypto.EncryptedString(`{"url":"https://example.com/webhook"}`),
	}
	if err := gorm.G[models.Notification](db.DB).Create(t.Context(), &notification); err != nil {
		t.Fatalf("failed to create notification: %v", err)
	}

	oidcProvider := models.OIDCProvider{
		Name:                 "oidc-name",
		IssuerURL:            "https://issuer.example.com",
		ClientId:             "client-id",
		ClientSecret:         crypto.EncryptedString("oidc-client-secret"),
		Scopes:               "openid email profile",
		Enabled:              true,
		RequireVerifiedEmail: true,
		AutoSignup:           true,
	}
	if err := gorm.G[models.OIDCProvider](db.DB).Create(t.Context(), &oidcProvider); err != nil {
		t.Fatalf("failed to create oidc provider: %v", err)
	}

	return keyRotateFixture{
		AgentID:        agent.Id,
		ApplicationID:  application.Id,
		RepositoryID:   repository.Id,
		NotificationID: notification.Id,
		OIDCProviderID: oidcProvider.Id,
	}
}

func TestRotateDatabaseEncryptionKeyReencryptsEncryptedFields(t *testing.T) {
	setupKeyRotateTestDB(t)
	fixture := seedKeyRotateFixture(t)

	result, err := rotateDatabaseEncryptionKey(t.Context(), testOldAppSecret, testNewAppSecret)
	if err != nil {
		t.Fatalf("rotateDatabaseEncryptionKey() unexpected error: %v", err)
	}

	if result.Agents != 1 || result.Applications != 1 || result.Repositories != 1 || result.Notifications != 1 || result.OIDCProviders != 1 {
		t.Fatalf("unexpected rotation result: %+v", result)
	}

	if err := crypto.Init(testNewAppSecret); err != nil {
		t.Fatalf("failed to init new crypto: %v", err)
	}

	agent, err := gorm.G[models.Agent](db.DB).Where("id = ?", fixture.AgentID).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load agent with new key: %v", err)
	}
	if agent.Name.String() != "agent-name" || agent.KeyId.String() != "agent-key-id" {
		t.Fatalf("unexpected agent values after rotation: %+v", agent)
	}

	application, err := gorm.G[models.Application](db.DB).Where("id = ?", fixture.ApplicationID).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load application with new key: %v", err)
	}
	if application.Name.String() != "application-name" ||
		application.ComposeFile.String() != "services:\n  app:\n    image: nginx\n" ||
		application.PreviousComposeFile.String() != "services: {}\n" ||
		application.ImageWebhookSecret == nil ||
		application.ImageWebhookSecret.String() != "image-webhook-secret" {
		t.Fatalf("unexpected application values after rotation: %+v", application)
	}

	repository, err := gorm.G[models.Repository](db.DB).Where("id = ?", fixture.RepositoryID).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load repository with new key: %v", err)
	}
	if repository.AuthUser == nil || repository.AuthUser.String() != "repo-user" ||
		repository.AuthToken == nil || repository.AuthToken.String() != "repo-token" ||
		repository.WebhookSecret == nil || repository.WebhookSecret.String() != "repo-webhook-secret" {
		t.Fatalf("unexpected repository values after rotation: %+v", repository)
	}

	notification, err := gorm.G[models.Notification](db.DB).Where("id = ?", fixture.NotificationID).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load notification with new key: %v", err)
	}
	if notification.Name.String() != "notification-name" ||
		notification.Config.String() != `{"url":"https://example.com/webhook"}` {
		t.Fatalf("unexpected notification values after rotation: %+v", notification)
	}

	oidcProvider, err := gorm.G[models.OIDCProvider](db.DB).Where("id = ?", fixture.OIDCProviderID).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load oidc provider with new key: %v", err)
	}
	if oidcProvider.ClientSecret.String() != "oidc-client-secret" {
		t.Fatalf("unexpected oidc provider values after rotation: %+v", oidcProvider)
	}

	var rawAgentKeyID string
	if err := db.DB.Table("agents").Select("key_id").Where("id = ?", fixture.AgentID).Scan(&rawAgentKeyID).Error; err != nil {
		t.Fatalf("failed to load raw agent key id: %v", err)
	}
	if err := crypto.Init(testOldAppSecret); err != nil {
		t.Fatalf("failed to re-init old crypto: %v", err)
	}
	if _, err := crypto.Decrypt(rawAgentKeyID); err == nil {
		t.Fatal("expected raw agent key id to reject the old encryption key")
	}
}

func TestRotateDatabaseEncryptionKeyRejectsInvalidSecrets(t *testing.T) {
	tests := []struct {
		name      string
		oldSecret string
		newSecret string
	}{
		{name: "missing old secret", oldSecret: "", newSecret: testNewAppSecret},
		{name: "missing new secret", oldSecret: testOldAppSecret, newSecret: ""},
		{name: "short old secret", oldSecret: "short", newSecret: testNewAppSecret},
		{name: "short new secret", oldSecret: testOldAppSecret, newSecret: "short"},
		{name: "same secret", oldSecret: testOldAppSecret, newSecret: testOldAppSecret},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateKeyRotateSecrets(tt.oldSecret, tt.newSecret); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestNewKeyCmdIncludesRotate(t *testing.T) {
	keyCmd := newKeyCmd()

	found, _, err := keyCmd.Find([]string{"rotate"})
	if err != nil {
		t.Fatalf("failed to find rotate command: %v", err)
	}
	if found == nil || found.Name() != "rotate" {
		t.Fatalf("expected rotate command, got %v", found)
	}
}
