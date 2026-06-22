package application_deployer

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/websocket"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func setupTestDB(t *testing.T) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	testDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: gormlogger.New(
			log.New(os.Stderr, "\n", log.LstdFlags),
			gormlogger.Config{LogLevel: gormlogger.Warn, IgnoreRecordNotFoundError: true},
		),
	})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	if err := testDB.AutoMigrate(&models.Agent{}, &models.Repository{}, &models.Application{}, &models.AuditLog{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
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
}

func seedApp(t *testing.T) models.Application {
	t.Helper()
	app := models.Application{
		Name:         crypto.EncryptedString("test-app"),
		AgentId:      "agent-1",
		SyncStatus:   models.UnknownSync,
		HealthStatus: models.UnknownHealth,
		Branch:       "main",
		Path:         "deploy.yml",
		ComposeFile:  crypto.EncryptedString("version: '3.9'\n"),
	}
	if err := db.DB.Select("*").Create(&app).Error; err != nil {
		t.Fatalf("failed to seed application: %v", err)
	}
	return app
}

func TestTriggerApplicationDeploy_MarksSyncing(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t)
	nop := zerolog.Nop()

	// Hub with no connected agents: Send returns false without panicking, so we can
	// assert the in-progress transition independently of agent connectivity.
	websocket.DefaultHub = websocket.NewHub(&nop)
	t.Cleanup(func() { websocket.DefaultHub = nil })

	d := NewApplicationDeployer(&nop)
	if err := d.TriggerApplicationDeploy(context.Background(), &app, "version: '3.9'\n"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(context.Background())
	if err != nil {
		t.Fatalf("failed to load application: %v", err)
	}
	if updated.SyncStatus != models.Syncing {
		t.Errorf("expected SyncStatus %q, got %q", models.Syncing, updated.SyncStatus)
	}
}
