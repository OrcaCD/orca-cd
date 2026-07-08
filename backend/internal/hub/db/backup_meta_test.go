package db

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// setBackupSchemaVersion opens a backup file read-write and overwrites its
// schema_migrations row so tests can simulate older/newer/dirty backups.
func setBackupSchemaVersion(t *testing.T, path string, version uint, dirty bool) {
	t.Helper()
	backupDB, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatalf("failed to open backup for mutation: %v", err)
	}
	defer func() {
		sqlDB, _ := backupDB.DB()
		_ = sqlDB.Close()
	}()

	if err := backupDB.Exec("UPDATE schema_migrations SET version = ?, dirty = ?", version, dirty).Error; err != nil {
		t.Fatalf("failed to update schema_migrations: %v", err)
	}
}

func newValidBackup(t *testing.T) string {
	t.Helper()
	setupGlobalDB(t)
	path := filepath.Join(t.TempDir(), "backup.db")
	if err := Export(path); err != nil {
		t.Fatalf("failed to export backup: %v", err)
	}
	return path
}

func TestCurrentSchemaVersion_MatchesMigratedDB(t *testing.T) {
	gormDB := openTestDB(t)
	if err := runMigrations(gormDB); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}

	current, err := CurrentSchemaVersion()
	if err != nil {
		t.Fatalf("CurrentSchemaVersion() error: %v", err)
	}
	if current == 0 {
		t.Fatal("CurrentSchemaVersion() = 0, want > 0")
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}
	var dbVersion uint
	if err := sqlDB.QueryRow("SELECT version FROM schema_migrations LIMIT 1").Scan(&dbVersion); err != nil {
		t.Fatalf("failed to read schema_migrations: %v", err)
	}
	if current != dbVersion {
		t.Errorf("CurrentSchemaVersion() = %d, migrated DB version = %d", current, dbVersion)
	}
}

func TestReadBackupSchemaVersion_MissingTable(t *testing.T) {
	// A bare sqlite DB with no migrations has no schema_migrations table.
	path := filepath.Join(t.TempDir(), "empty.db")
	bare, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatalf("failed to create bare db: %v", err)
	}
	if err := bare.Exec("CREATE TABLE placeholder (id INTEGER)").Error; err != nil {
		t.Fatalf("failed to create placeholder table: %v", err)
	}
	sqlDB, _ := bare.DB()
	_ = sqlDB.Close()

	if _, _, err := readBackupSchemaVersion(path); !errors.Is(err, ErrNotOrcaBackup) {
		t.Fatalf("expected ErrNotOrcaBackup, got %v", err)
	}
}

func TestReadBackupSchemaVersion_OpenError(t *testing.T) {
	// Point at a directory: sqlite cannot open it as a database file, so
	// gorm.Open fails during initialization.
	_, _, err := readBackupSchemaVersion(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "failed to open backup database") {
		t.Fatalf("expected open error, got %v", err)
	}
}

func TestReadBackupSchemaVersion_QueryError(t *testing.T) {
	// schema_migrations exists but lacks the expected version/dirty columns, so
	// the SELECT fails at query time rather than the HasTable guard.
	path := filepath.Join(t.TempDir(), "malformed.db")
	bad, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	if err := bad.Exec("CREATE TABLE schema_migrations (other INTEGER)").Error; err != nil {
		t.Fatalf("failed to create malformed table: %v", err)
	}
	sqlDB, _ := bad.DB()
	_ = sqlDB.Close()

	_, _, err = readBackupSchemaVersion(path)
	if err == nil || !strings.Contains(err.Error(), "failed to read backup schema version") {
		t.Fatalf("expected query error, got %v", err)
	}
}

func TestValidateBackup_PropagatesReadError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.db")
	if err := ValidateBackup(path); err == nil {
		t.Fatal("expected error for unreadable backup, got nil")
	}
}

func TestValidateBackup_AcceptsEqualVersion(t *testing.T) {
	path := newValidBackup(t)
	if err := ValidateBackup(path); err != nil {
		t.Fatalf("ValidateBackup() unexpected error for equal-version backup: %v", err)
	}
}

func TestValidateBackup_AcceptsOlderVersion(t *testing.T) {
	path := newValidBackup(t)
	setBackupSchemaVersion(t, path, 1, false)
	if err := ValidateBackup(path); err != nil {
		t.Fatalf("ValidateBackup() unexpected error for older backup: %v", err)
	}
}

func TestValidateBackup_RejectsNewerVersion(t *testing.T) {
	path := newValidBackup(t)
	current, err := CurrentSchemaVersion()
	if err != nil {
		t.Fatalf("CurrentSchemaVersion() error: %v", err)
	}
	setBackupSchemaVersion(t, path, current+5, false)

	err = ValidateBackup(path)
	if err == nil || !strings.Contains(err.Error(), "newer OrcaCD") {
		t.Fatalf("expected newer-version rejection, got %v", err)
	}
}

func TestValidateBackup_RejectsDirty(t *testing.T) {
	path := newValidBackup(t)
	setBackupSchemaVersion(t, path, 1, true)

	err := ValidateBackup(path)
	if err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("expected dirty rejection, got %v", err)
	}
}

func TestValidateBackup_RejectsNonBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notabackup.db")
	bare, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatalf("failed to create bare db: %v", err)
	}
	if err := bare.Exec("CREATE TABLE placeholder (id INTEGER)").Error; err != nil {
		t.Fatalf("failed to create placeholder table: %v", err)
	}
	sqlDB, _ := bare.DB()
	_ = sqlDB.Close()

	if err := ValidateBackup(path); !errors.Is(err, ErrNotOrcaBackup) {
		t.Fatalf("expected ErrNotOrcaBackup, got %v", err)
	}
}
