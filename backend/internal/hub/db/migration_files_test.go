package db

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func setAllowDowngrade(t *testing.T, allow bool) {
	t.Helper()
	previous := AllowDowngrade
	AllowDowngrade = allow
	t.Cleanup(func() { AllowDowngrade = previous })
}

func latestSchemaVersion(t *testing.T) uint {
	t.Helper()
	latest, err := CurrentSchemaVersion()
	if err != nil {
		t.Fatalf("CurrentSchemaVersion() error: %v", err)
	}
	return latest
}

func schemaVersion(t *testing.T, gormDB *gorm.DB) (uint, bool) {
	t.Helper()
	row, err := gorm.G[schemaMigration](gormDB).Raw("SELECT version, dirty FROM schema_migrations LIMIT 1").First(context.Background())
	if err != nil {
		t.Fatalf("failed to read schema version: %v", err)
	}
	return row.Version, row.Dirty
}

// applyNewerMigration simulates a newer OrcaCD version that applied one more migration,
// optionally storing its files like that version would.
func applyNewerMigration(t *testing.T, gormDB *gorm.DB, storeFiles bool) uint {
	t.Helper()
	if err := runMigrations(gormDB); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}

	version := latestSchemaVersion(t) + 1
	if err := gormDB.Exec("CREATE TABLE downgrade_probe (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatalf("failed to apply newer migration: %v", err)
	}
	if err := gormDB.Exec("UPDATE schema_migrations SET version = ?, dirty = false", version).Error; err != nil {
		t.Fatalf("failed to set schema version: %v", err)
	}

	if storeFiles {
		files := []schemaMigrationFile{
			{Name: fmt.Sprintf("%06d_downgrade_probe.up.sql", version), Version: version, Content: "CREATE TABLE downgrade_probe (id INTEGER PRIMARY KEY);"},
			{Name: fmt.Sprintf("%06d_downgrade_probe.down.sql", version), Version: version, Content: "DROP TABLE downgrade_probe;"},
		}
		if err := gorm.G[schemaMigrationFile](gormDB).CreateInBatches(context.Background(), &files, 10); err != nil {
			t.Fatalf("failed to store newer migration files: %v", err)
		}
	}
	return version
}

func TestRunMigrations_StoresMigrationFiles(t *testing.T) {
	gormDB := openTestDB(t)
	if err := runMigrations(gormDB); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}

	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		t.Fatalf("failed to read embedded migrations: %v", err)
	}
	stored, err := gorm.G[schemaMigrationFile](gormDB).Find(context.Background())
	if err != nil {
		t.Fatalf("failed to load stored migration files: %v", err)
	}
	if len(stored) != len(entries) {
		t.Fatalf("stored %d migration files, want %d", len(stored), len(entries))
	}

	for _, file := range stored {
		content, err := migrationFiles.ReadFile("migrations/" + file.Name)
		if err != nil {
			t.Fatalf("stored migration file %q is not embedded: %v", file.Name, err)
		}
		if file.Content != string(content) {
			t.Errorf("stored content of %q differs from the embedded file", file.Name)
		}
		if !strings.HasPrefix(file.Name, fmt.Sprintf("%06d_", file.Version)) {
			t.Errorf("stored migration file %q has version %d", file.Name, file.Version)
		}
	}
}

func TestRunMigrations_RestoresChangedMigrationFiles(t *testing.T) {
	gormDB := openTestDB(t)
	if err := runMigrations(gormDB); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}

	ctx := context.Background()
	if _, err := gorm.G[schemaMigrationFile](gormDB).Where("version = ?", 1).Update(ctx, "content", "changed"); err != nil {
		t.Fatalf("failed to change stored migration file: %v", err)
	}
	if err := gormDB.Exec("INSERT INTO schema_migration_files (name, version, content) VALUES ('000001_stale.up.sql', 1, '')").Error; err != nil {
		t.Fatalf("failed to store stale migration file: %v", err)
	}
	if err := runMigrations(gormDB); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}

	stored, err := gorm.G[schemaMigrationFile](gormDB).Where("version = ?", 1).Find(ctx)
	if err != nil {
		t.Fatalf("failed to load stored migration files: %v", err)
	}
	if len(stored) != 2 {
		t.Fatalf("stored %d files for migration 1, want 2", len(stored))
	}
	for _, file := range stored {
		content, err := migrationFiles.ReadFile("migrations/" + file.Name)
		if err != nil {
			t.Fatalf("stored migration file %q is not embedded: %v", file.Name, err)
		}
		if file.Content != string(content) {
			t.Errorf("stored content of %q was not restored", file.Name)
		}
	}
}

func TestRunMigrations_KeepsStoredFilesOfNewerVersions(t *testing.T) {
	gormDB := openTestDB(t)
	version := applyNewerMigration(t, gormDB, true)

	// Rewinding the version lets runMigrations succeed without rolling back
	if err := gormDB.Exec("UPDATE schema_migrations SET version = ?", version-1).Error; err != nil {
		t.Fatalf("failed to set schema version: %v", err)
	}
	if err := runMigrations(gormDB); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}

	stored, err := gorm.G[schemaMigrationFile](gormDB).Where("version = ?", version).Find(context.Background())
	if err != nil {
		t.Fatalf("failed to load stored migration files: %v", err)
	}
	if len(stored) != 2 {
		t.Errorf("expected the 2 files of the newer migration to be kept, got %d", len(stored))
	}
}

func TestRunMigrations_AllMigrationsRollBack(t *testing.T) {
	gormDB := openTestDB(t)
	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}

	m := newTestMigrator(t, sqlDB)
	if err := m.Up(); err != nil {
		t.Fatalf("Up() error: %v", err)
	}
	if err := m.Down(); err != nil {
		t.Fatalf("Down() error: %v", err)
	}
	if gormDB.Migrator().HasTable("agents") {
		t.Error("expected agents table to be dropped after rolling back all migrations")
	}
	if err := m.Up(); err != nil {
		t.Fatalf("Up() after Down() error: %v", err)
	}
}

func TestRunMigrations_RefusesNewerSchemaWithoutAllowDowngrade(t *testing.T) {
	setAllowDowngrade(t, false)
	gormDB := openTestDB(t)
	version := applyNewerMigration(t, gormDB, true)

	err := runMigrations(gormDB)
	if err == nil || !strings.Contains(err.Error(), "ALLOW_DOWNGRADE=true") {
		t.Fatalf("runMigrations() error = %v, want an error mentioning ALLOW_DOWNGRADE", err)
	}

	if got, _ := schemaVersion(t, gormDB); got != version {
		t.Errorf("schema version = %d, want %d", got, version)
	}
	if !gormDB.Migrator().HasTable("downgrade_probe") {
		t.Error("expected the newer migration to be left in place")
	}
}

func TestRunMigrations_RollsBackNewerSchema(t *testing.T) {
	setAllowDowngrade(t, true)
	gormDB := openTestDB(t)
	version := applyNewerMigration(t, gormDB, true)

	if err := runMigrations(gormDB); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}

	latest := latestSchemaVersion(t)
	if got, dirty := schemaVersion(t, gormDB); got != latest || dirty {
		t.Errorf("schema version = %d (dirty %v), want %d", got, dirty, latest)
	}
	if gormDB.Migrator().HasTable("downgrade_probe") {
		t.Error("expected the newer migration to be rolled back")
	}

	stored, err := gorm.G[schemaMigrationFile](gormDB).Where("version > ?", latest).Find(context.Background())
	if err != nil {
		t.Fatalf("failed to load stored migration files: %v", err)
	}
	if len(stored) != 0 {
		t.Errorf("expected the files of rolled back migrations to be removed, got %d", len(stored))
	}

	main, err := gorm.G[databaseFile](gormDB).Raw("SELECT file FROM pragma_database_list WHERE name = 'main'").First(context.Background())
	if err != nil {
		t.Fatalf("failed to determine database file: %v", err)
	}
	backups, err := filepath.Glob(fmt.Sprintf("%s-downgrade-v%d-*.bak", main.File, version))
	if err != nil {
		t.Fatalf("failed to look for backups: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected 1 backup before rolling back, got %d", len(backups))
	}

	info, err := os.Stat(backups[0])
	if err != nil {
		t.Fatalf("failed to stat backup: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("backup permissions = %o, want 600", perm)
	}

	backupVersion, _, err := readBackupSchemaVersion(backups[0])
	if err != nil {
		t.Fatalf("failed to read backup schema version: %v", err)
	}
	if backupVersion != version {
		t.Errorf("backup schema version = %d, want %d", backupVersion, version)
	}
}

func TestRunMigrations_RefusesRollbackWithoutStoredFiles(t *testing.T) {
	setAllowDowngrade(t, true)
	gormDB := openTestDB(t)
	version := applyNewerMigration(t, gormDB, false)

	err := runMigrations(gormDB)
	if err == nil || !strings.Contains(err.Error(), "did not store its files") {
		t.Fatalf("runMigrations() error = %v, want an error about missing migration files", err)
	}
	if got, _ := schemaVersion(t, gormDB); got != version {
		t.Errorf("schema version = %d, want %d", got, version)
	}
}

func TestRunMigrations_RefusesRollbackOfDirtySchema(t *testing.T) {
	// The dirty state is reported first, as allowing downgrades wouldn't help
	setAllowDowngrade(t, false)
	gormDB := openTestDB(t)
	applyNewerMigration(t, gormDB, true)
	if err := gormDB.Exec("UPDATE schema_migrations SET dirty = true").Error; err != nil {
		t.Fatalf("failed to mark schema dirty: %v", err)
	}

	err := runMigrations(gormDB)
	if err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("runMigrations() error = %v, want an error about the dirty schema", err)
	}
	if !gormDB.Migrator().HasTable("downgrade_probe") {
		t.Error("expected the newer migration to be left in place")
	}
}

func TestRunMigrations_ReportsFailedRollback(t *testing.T) {
	setAllowDowngrade(t, true)
	gormDB := openTestDB(t)
	version := applyNewerMigration(t, gormDB, true)
	if _, err := gorm.G[schemaMigrationFile](gormDB).
		Where("name = ?", fmt.Sprintf("%06d_downgrade_probe.down.sql", version)).
		Update(context.Background(), "content", "DROP TABLE missing_table;"); err != nil {
		t.Fatalf("failed to break down migration: %v", err)
	}

	err := runMigrations(gormDB)
	if err == nil || !strings.Contains(err.Error(), "failed to roll back") || !strings.Contains(err.Error(), "backup at") {
		t.Fatalf("runMigrations() error = %v, want a rollback error naming the backup", err)
	}
	// golang-migrate marks the version it rolls back to as dirty, so the next start refuses to migrate
	if _, dirty := schemaVersion(t, gormDB); !dirty {
		t.Error("expected the schema to be dirty after a failed rollback")
	}
	stored, err := gorm.G[schemaMigrationFile](gormDB).Where("version = ?", version).Find(context.Background())
	if err != nil {
		t.Fatalf("failed to load stored migration files: %v", err)
	}
	if len(stored) != 2 {
		t.Errorf("expected the files of the failed rollback to be kept, got %d", len(stored))
	}
}

func TestBackupBeforeDowngrade_RefusesInMemoryDatabase(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := gormDB.DB()
		_ = sqlDB.Close()
	})

	if _, err := backupBeforeDowngrade(context.Background(), gormDB, 1); err == nil || !strings.Contains(err.Error(), "in-memory") {
		t.Fatalf("backupBeforeDowngrade() error = %v, want an error about the in-memory database", err)
	}
}
