package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/rs/zerolog"
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

type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

func setupKeyRotateCommandEnv(t *testing.T) string {
	t.Helper()

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	workDir := t.TempDir()
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("failed to change working directory: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workDir, "data"), 0750); err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		_ = os.Chdir(originalWD)
	})

	t.Setenv("APP_URL", "http://localhost")
	t.Setenv("APP_SECRET", testNewAppSecret)
	return workDir
}

func setupKeyRotateTestDB(t *testing.T) {
	t.Helper()
	setupKeyRotateTestDBWithLogger(t, gormlogger.Discard)
}

func setupKeyRotateTestDBWithLogger(t *testing.T, logger gormlogger.Interface) {
	t.Helper()

	testDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{Logger: logger})
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

type countingGormLogger struct {
	updates int
}

func (l *countingGormLogger) LogMode(gormlogger.LogLevel) gormlogger.Interface {
	return l
}

func (l *countingGormLogger) Info(context.Context, string, ...any) {}

func (l *countingGormLogger) Warn(context.Context, string, ...any) {}

func (l *countingGormLogger) Error(context.Context, string, ...any) {}

func (l *countingGormLogger) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, _ := fc()
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(sql)), "UPDATE ") {
		l.updates++
	}
}

func (l *countingGormLogger) Reset() {
	l.updates = 0
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

func seedKeyRotateCommandDatabase(t *testing.T) keyRotateFixture {
	t.Helper()

	if err := crypto.Init(testOldAppSecret); err != nil {
		t.Fatalf("failed to init old crypto: %v", err)
	}

	if err := db.Connect(zerolog.New(io.Discard), zerolog.Disabled, false); err != nil {
		t.Fatalf("failed to connect command database: %v", err)
	}
	fixture := seedKeyRotateFixture(t)
	if err := db.Close(); err != nil {
		t.Fatalf("failed to close command database: %v", err)
	}
	return fixture
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

func TestRotateDatabaseEncryptionKeyUsesOneUpdatePerTouchedRow(t *testing.T) {
	logger := &countingGormLogger{}
	setupKeyRotateTestDBWithLogger(t, logger)
	seedKeyRotateFixture(t)
	logger.Reset()

	if _, err := rotateDatabaseEncryptionKey(t.Context(), testOldAppSecret, testNewAppSecret); err != nil {
		t.Fatalf("rotateDatabaseEncryptionKey() unexpected error: %v", err)
	}

	if logger.updates != 5 {
		t.Fatalf("expected one update for each touched encrypted row, got %d updates", logger.updates)
	}
}

func TestRunKeyRotateCommandSucceeds(t *testing.T) {
	setupKeyRotateCommandEnv(t)
	fixture := seedKeyRotateCommandDatabase(t)

	var out bytes.Buffer
	err := runKeyRotateCommandWithInput(t.Context(), &out, strings.NewReader("yes\n"), keyRotateOptions{
		OldSecret:        "  " + testOldAppSecret + "  ",
		SkipConfirmation: true,
	})
	if err != nil {
		t.Fatalf("runKeyRotateCommandWithInput() unexpected error: %v", err)
	}

	if !strings.Contains(out.String(), "Key Rotation Successful") {
		t.Fatalf("expected success output, got %q", out.String())
	}
	if !strings.Contains(out.String(), "Rows re-encrypted:") {
		t.Fatalf("expected output to include row count, got %q", out.String())
	}

	if err := crypto.Init(testNewAppSecret); err != nil {
		t.Fatalf("failed to init new crypto: %v", err)
	}
	if err := db.Connect(zerolog.New(io.Discard), zerolog.Disabled, false); err != nil {
		t.Fatalf("failed to reconnect command database: %v", err)
	}
	agent, err := gorm.G[models.Agent](db.DB).Where("id = ?", fixture.AgentID).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load rotated agent: %v", err)
	}
	if agent.KeyId.String() != "agent-key-id" {
		t.Fatalf("expected rotated agent key id, got %q", agent.KeyId.String())
	}
}

func TestRunKeyRotateCommandRejectsInvalidConfiguration(t *testing.T) {
	setupKeyRotateCommandEnv(t)
	t.Setenv("APP_SECRET", "")

	var out bytes.Buffer
	err := runKeyRotateCommandWithInput(t.Context(), &out, strings.NewReader("yes\n"), keyRotateOptions{
		OldSecret:        testOldAppSecret,
		SkipConfirmation: true,
	})
	if err == nil {
		t.Fatal("runKeyRotateCommandWithInput() expected configuration error, got nil")
	}
	if !strings.Contains(err.Error(), "configuration") {
		t.Fatalf("expected configuration error, got %v", err)
	}
}

func TestRunKeyRotateCommandRejectsInvalidOldSecret(t *testing.T) {
	setupKeyRotateCommandEnv(t)

	var out bytes.Buffer
	err := runKeyRotateCommandWithInput(t.Context(), &out, strings.NewReader("yes\n"), keyRotateOptions{
		OldSecret:        "short",
		SkipConfirmation: true,
	})
	if err == nil {
		t.Fatal("runKeyRotateCommandWithInput() expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid old secret") {
		t.Fatalf("expected invalid old secret error, got %v", err)
	}
}

func TestRunKeyRotateCommandReturnsConfirmationReadError(t *testing.T) {
	setupKeyRotateCommandEnv(t)

	var out bytes.Buffer
	err := runKeyRotateCommandWithInput(t.Context(), &out, errReader{}, keyRotateOptions{
		OldSecret: testOldAppSecret,
	})
	if err == nil {
		t.Fatal("runKeyRotateCommandWithInput() expected confirmation read error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to read confirmation") {
		t.Fatalf("expected confirmation read error, got %v", err)
	}
}

func TestRunKeyRotateCommandCancelsWhenUserDeclines(t *testing.T) {
	setupKeyRotateCommandEnv(t)

	var out bytes.Buffer
	err := runKeyRotateCommandWithInput(t.Context(), &out, strings.NewReader("no\n"), keyRotateOptions{
		OldSecret: testOldAppSecret,
	})
	if err != nil {
		t.Fatalf("runKeyRotateCommandWithInput() unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "Key rotation cancelled") {
		t.Fatalf("expected cancellation output, got %q", out.String())
	}
}

func TestRunKeyRotateCommandReturnsDatabaseConnectError(t *testing.T) {
	workDir := setupKeyRotateCommandEnv(t)
	if err := os.RemoveAll(filepath.Join(workDir, "data")); err != nil {
		t.Fatalf("failed to remove data dir: %v", err)
	}

	var out bytes.Buffer
	err := runKeyRotateCommandWithInput(t.Context(), &out, strings.NewReader("yes\n"), keyRotateOptions{
		OldSecret:        testOldAppSecret,
		SkipConfirmation: true,
	})
	if err == nil {
		t.Fatal("runKeyRotateCommandWithInput() expected database connect error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to connect to database") {
		t.Fatalf("expected database connect error, got %v", err)
	}
}

func TestRunKeyRotateCommandReturnsRotationError(t *testing.T) {
	setupKeyRotateCommandEnv(t)
	seedKeyRotateCommandDatabase(t)

	var out bytes.Buffer
	err := runKeyRotateCommandWithInput(t.Context(), &out, strings.NewReader("yes\n"), keyRotateOptions{
		OldSecret:        "wrong-old-secret-that-is-long-enough",
		SkipConfirmation: true,
	})
	if err == nil {
		t.Fatal("runKeyRotateCommandWithInput() expected rotation error, got nil")
	}
	if !strings.Contains(err.Error(), "key rotation failed") {
		t.Fatalf("expected key rotation error, got %v", err)
	}
}

func TestRunKeyRotateCommandRejectsDemoMode(t *testing.T) {
	setupKeyRotateCommandEnv(t)
	t.Setenv("DEMO", "true")

	var out bytes.Buffer
	err := runKeyRotateCommandWithInput(t.Context(), &out, strings.NewReader("yes\n"), keyRotateOptions{
		OldSecret:        testOldAppSecret,
		SkipConfirmation: true,
	})
	if err == nil {
		t.Fatal("runKeyRotateCommandWithInput() expected error in demo mode, got nil")
	}
	if !strings.Contains(err.Error(), "demo mode") {
		t.Fatalf("expected demo mode error, got %v", err)
	}
}

func TestRotateDatabaseEncryptionKeyRejectsMissingDatabase(t *testing.T) {
	originalDB := db.DB
	db.DB = nil
	t.Cleanup(func() {
		db.DB = originalDB
	})

	_, err := rotateDatabaseEncryptionKey(t.Context(), testOldAppSecret, testNewAppSecret)
	if err == nil {
		t.Fatal("rotateDatabaseEncryptionKey() expected error without database, got nil")
	}
	if !strings.Contains(err.Error(), "database is not connected") {
		t.Fatalf("expected missing database error, got %v", err)
	}
}

func TestRotateEncryptedBatchesPropagatesRotateError(t *testing.T) {
	setupKeyRotateTestDB(t)
	seedKeyRotateFixture(t)

	wantErr := errors.New("rotate batch failed")
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		return rotateEncryptedBatches[models.Agent](t.Context(), tx, testOldAppSecret, testNewAppSecret, "agents", func([]models.Agent) error {
			return wantErr
		})
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected rotate error %v, got %v", wantErr, err)
	}
}

func TestRotateRepositoriesSkipsRowsWithoutEncryptedFields(t *testing.T) {
	setupKeyRotateTestDB(t)
	repositories := []models.Repository{{
		Base: models.Base{Id: "repo-without-secrets"},
	}}
	var result keyRotationResult

	if err := rotateRepositories(t.Context(), db.DB, repositories, &result); err != nil {
		t.Fatalf("rotateRepositories() unexpected error: %v", err)
	}
	if result.Repositories != 0 {
		t.Fatalf("expected no repository updates, got %d", result.Repositories)
	}
}

func TestRotateHelpersReturnNotFound(t *testing.T) {
	setupKeyRotateTestDB(t)
	if err := crypto.Init(testNewAppSecret); err != nil {
		t.Fatalf("failed to init crypto: %v", err)
	}

	authToken := crypto.EncryptedString("token")
	tests := []struct {
		name string
		run  func(*keyRotationResult) error
	}{
		{
			name: "agent",
			run: func(result *keyRotationResult) error {
				return rotateAgents(t.Context(), db.DB, []models.Agent{{
					Base:  models.Base{Id: "missing-agent"},
					Name:  crypto.EncryptedString("agent"),
					KeyId: crypto.EncryptedString("key"),
				}}, result)
			},
		},
		{
			name: "application",
			run: func(result *keyRotationResult) error {
				return rotateApplications(t.Context(), db.DB, []models.Application{{
					Base:                models.Base{Id: "missing-application"},
					Name:                crypto.EncryptedString("app"),
					ComposeFile:         crypto.EncryptedString("services: {}"),
					PreviousComposeFile: crypto.EncryptedString(""),
				}}, result)
			},
		},
		{
			name: "repository",
			run: func(result *keyRotationResult) error {
				return rotateRepositories(t.Context(), db.DB, []models.Repository{{
					Base:      models.Base{Id: "missing-repository"},
					AuthToken: &authToken,
				}}, result)
			},
		},
		{
			name: "notification",
			run: func(result *keyRotationResult) error {
				return rotateNotifications(t.Context(), db.DB, []models.Notification{{
					Base:   models.Base{Id: "missing-notification"},
					Name:   crypto.EncryptedString("notification"),
					Config: crypto.EncryptedString("{}"),
				}}, result)
			},
		},
		{
			name: "oidc provider",
			run: func(result *keyRotationResult) error {
				return rotateOIDCProviders(t.Context(), db.DB, []models.OIDCProvider{{
					Base:         models.Base{Id: "missing-oidc-provider"},
					ClientSecret: crypto.EncryptedString("secret"),
				}}, result)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result keyRotationResult
			err := tt.run(&result)
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("expected record not found, got %v", err)
			}
		})
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

func TestKeyRotationResultTotal(t *testing.T) {
	result := keyRotationResult{
		Agents:        1,
		Applications:  2,
		Repositories:  3,
		Notifications: 4,
		OIDCProviders: 5,
	}

	if result.Total() != 15 {
		t.Fatalf("Total() = %d, want 15", result.Total())
	}
}

func TestRenderKeyRotateResult(t *testing.T) {
	var out bytes.Buffer
	renderKeyRotateResult(&out, keyRotationResult{
		Agents:        1,
		Applications:  2,
		Repositories:  3,
		Notifications: 4,
		OIDCProviders: 5,
	})

	got := out.String()
	for _, want := range []string{
		"Key Rotation Successful",
		"Rows re-encrypted:",
		"15",
		"Restart the hub with the new APP_SECRET.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected output to contain %q, got %q", want, got)
		}
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

func TestNewRootCmdIncludesKeyRotate(t *testing.T) {
	rootCmd := newRootCmd()

	found, _, err := rootCmd.Find([]string{"key", "rotate"})
	if err != nil {
		t.Fatalf("failed to find key rotate command: %v", err)
	}
	if found == nil || found.Name() != "rotate" {
		t.Fatalf("expected key rotate command, got %v", found)
	}
}
