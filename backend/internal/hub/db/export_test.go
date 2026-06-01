package db

import (
	"os"
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestExport_Succeeds(t *testing.T) {
	setupGlobalDB(t)

	outputPath := filepath.Join(t.TempDir(), "export.db")
	if err := Export(outputPath); err != nil {
		t.Fatalf("Export() unexpected error: %v", err)
	}

	if _, err := os.Stat(outputPath); err != nil {
		t.Errorf("expected export file to exist at %q: %v", outputPath, err)
	}
}

func TestExport_FilePassesIntegrityCheck(t *testing.T) {
	setupGlobalDB(t)

	outputPath := filepath.Join(t.TempDir(), "export.db")
	if err := Export(outputPath); err != nil {
		t.Fatalf("Export() unexpected error: %v", err)
	}

	exportDB, err := gorm.Open(sqlite.Open(outputPath), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatalf("failed to open export database: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := exportDB.DB()
		_ = sqlDB.Close()
	})

	var result string
	if err := exportDB.Raw("PRAGMA integrity_check").Scan(&result).Error; err != nil {
		t.Fatalf("PRAGMA integrity_check query failed: %v", err)
	}
	if result != "ok" {
		t.Errorf("PRAGMA integrity_check = %q, want %q", result, "ok")
	}
}

func TestExport_FailsWhenDBNil(t *testing.T) {
	originalDB := DB
	DB = nil
	t.Cleanup(func() { DB = originalDB })

	outputPath := filepath.Join(t.TempDir(), "export.db")
	if err := Export(outputPath); err == nil {
		t.Fatal("Export() expected error when DB is nil, got nil")
	}
}

func TestExport_FailsOnClosedDB(t *testing.T) {
	setupGlobalDB(t)

	sqlDB, err := DB.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("failed to close sql.DB: %v", err)
	}

	outputPath := filepath.Join(t.TempDir(), "export.db")
	if err := Export(outputPath); err == nil {
		t.Fatal("Export() expected error on closed DB, got nil")
	}
}
