package db

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// schemaMigration mirrors golang-migrate's schema_migrations table.
type schemaMigration struct {
	Version uint
	Dirty   bool
}

var ErrNotOrcaBackup = errors.New("not a valid OrcaCD backup: missing schema_migrations table")

// CurrentSchemaVersion returns the highest migration version embedded in this
// binary, derived from the numeric prefix of the migration files
// (e.g. "000023_..." -> 23). It is the schema version the running binary knows
// how to operate on.
func CurrentSchemaVersion() (uint, error) {
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return 0, fmt.Errorf("failed to read embedded migrations: %w", err)
	}

	var max uint = 0
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		max++
	}

	if max == 0 {
		return 0, errors.New("no migrations found in embedded filesystem")
	}
	return max, nil
}

func readBackupSchemaVersion(path string) (version uint, dirty bool, err error) {
	backupDB, err := gorm.Open(sqlite.Open(path+"?mode=ro"), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		return 0, false, fmt.Errorf("failed to open backup database: %w", err)
	}
	defer func() {
		if sqlDB, derr := backupDB.DB(); derr == nil {
			_ = sqlDB.Close()
		}
	}()

	if !backupDB.Migrator().HasTable("schema_migrations") {
		return 0, false, ErrNotOrcaBackup
	}

	row, err := gorm.G[schemaMigration](backupDB).Raw("SELECT version, dirty FROM schema_migrations LIMIT 1").First(context.Background())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, false, ErrNotOrcaBackup
		}
		return 0, false, fmt.Errorf("failed to read backup schema version: %w", err)
	}

	return row.Version, row.Dirty, nil
}

func ValidateBackup(backupPath string) error {
	current, err := CurrentSchemaVersion()
	if err != nil {
		return fmt.Errorf("failed to determine current schema version: %w", err)
	}

	version, dirty, err := readBackupSchemaVersion(backupPath)
	if err != nil {
		return err
	}

	if dirty {
		return fmt.Errorf("backup is in a dirty migration state (schema v%d) and cannot be imported", version)
	}

	if version > current {
		return fmt.Errorf(
			"backup was created by a newer OrcaCD (schema v%d); this binary supports up to schema v%d — upgrade OrcaCD to import this backup",
			version, current,
		)
	}

	return nil
}
