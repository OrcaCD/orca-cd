package db

import (
	"os"
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestBackup_Succeeds(t *testing.T) {
	setupGlobalDB(t)

	outputPath := filepath.Join(t.TempDir(), "backup.db")
	if err := Backup(outputPath); err != nil {
		t.Fatalf("Backup() unexpected error: %v", err)
	}

	if _, err := os.Stat(outputPath); err != nil {
		t.Errorf("expected backup file to exist at %q: %v", outputPath, err)
	}
}

func TestBackup_FilePassesIntegrityCheck(t *testing.T) {
	setupGlobalDB(t)

	outputPath := filepath.Join(t.TempDir(), "backup.db")
	if err := Backup(outputPath); err != nil {
		t.Fatalf("Backup() unexpected error: %v", err)
	}

	backupDB, err := gorm.Open(sqlite.Open(outputPath), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatalf("failed to open backup database: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := backupDB.DB()
		_ = sqlDB.Close()
	})

	var result string
	if err := backupDB.Raw("PRAGMA integrity_check").Scan(&result).Error; err != nil {
		t.Fatalf("PRAGMA integrity_check query failed: %v", err)
	}
	if result != "ok" {
		t.Errorf("PRAGMA integrity_check = %q, want %q", result, "ok")
	}
}

func TestBackup_FailsWhenDBNil(t *testing.T) {
	originalDB := DB
	DB = nil
	t.Cleanup(func() { DB = originalDB })

	outputPath := filepath.Join(t.TempDir(), "backup.db")
	if err := Backup(outputPath); err == nil {
		t.Fatal("Backup() expected error when DB is nil, got nil")
	}
}

func TestBackup_FailsOnClosedDB(t *testing.T) {
	setupGlobalDB(t)

	sqlDB, err := DB.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("failed to close sql.DB: %v", err)
	}

	outputPath := filepath.Join(t.TempDir(), "backup.db")
	if err := Backup(outputPath); err == nil {
		t.Fatal("Backup() expected error on closed DB, got nil")
	}
}
