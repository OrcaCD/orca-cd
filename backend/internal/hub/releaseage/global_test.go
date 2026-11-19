package releaseage

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func setupTestDB(t *testing.T) {
	t.Helper()
	testDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "settings.db")), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := testDB.AutoMigrate(&models.Settings{}); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	sqlDB, err := testDB.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	previousDB := db.DB
	db.DB = testDB
	t.Cleanup(func() {
		db.DB = previousDB
		_ = sqlDB.Close()
	})
}

func TestGlobal_WithoutDatabase(t *testing.T) {
	previousDB := db.DB
	db.DB = nil
	t.Cleanup(func() { db.DB = previousDB })

	settings, err := Global(t.Context())
	if err != nil || settings.Id != models.SettingsId || settings.MinimumReleaseAgeSeconds != 0 {
		t.Errorf("Global = %+v, %v, want disabled settings", settings, err)
	}
}

func TestGlobal(t *testing.T) {
	setupTestDB(t)

	// Without a row the release age is disabled
	settings, err := Global(t.Context())
	if err != nil || settings.Id != models.SettingsId || settings.MinimumReleaseAgeSeconds != 0 {
		t.Fatalf("Global = %+v, %v, want disabled settings", settings, err)
	}

	stored := models.Settings{Id: models.SettingsId, MinimumReleaseAgeSeconds: 86400, ReleaseAgeAllowedRegistries: []string{"registry.local"}}
	if err := db.DB.Save(&stored).Error; err != nil {
		t.Fatalf("save settings: %v", err)
	}
	settings, err = Global(t.Context())
	if err != nil {
		t.Fatalf("Global: %v", err)
	}
	if settings.MinimumReleaseAgeSeconds != 86400 || !slices.Equal(settings.ReleaseAgeAllowedRegistries, stored.ReleaseAgeAllowedRegistries) {
		t.Errorf("Global = %+v, want %+v", settings, stored)
	}
}
