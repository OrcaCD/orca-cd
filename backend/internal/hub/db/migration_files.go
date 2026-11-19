package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	gofs "io/fs"
	"testing/fstest"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"gorm.io/gorm"
)

// AllowDowngrade lets the hub roll back migrations that a newer OrcaCD version applied.
var AllowDowngrade bool

// schemaMigrationFile is a migration file stored in the database. Every OrcaCD version
// stores the files it ships, so an older version can roll back migrations it doesn't know.
type schemaMigrationFile struct {
	Name    string `gorm:"primaryKey"`
	Version uint
	Content string
}

func (schemaMigrationFile) TableName() string {
	return "schema_migration_files"
}

type databaseFile struct {
	File string
}

// storeMigrationFiles replaces the stored files of the versions this binary ships with
// the embedded ones. Files of newer versions are kept, as they are needed to roll them back.
func storeMigrationFiles(ctx context.Context, db *gorm.DB, latest uint) error {
	// Created here instead of in a migration, so every version can rely on the table before migrating
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migration_files (
		name TEXT PRIMARY KEY,
		version INTEGER NOT NULL,
		content TEXT NOT NULL
	)`).Error; err != nil {
		return fmt.Errorf("failed to create migration files table: %w", err)
	}

	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read embedded migrations: %w", err)
	}

	files := make([]schemaMigrationFile, 0, len(entries))
	for _, entry := range entries {
		migration, err := source.Parse(entry.Name())
		if err != nil {
			return fmt.Errorf("failed to parse migration file %q: %w", entry.Name(), err)
		}
		content, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("failed to read migration file %q: %w", entry.Name(), err)
		}
		files = append(files, schemaMigrationFile{
			Name:    entry.Name(),
			Version: migration.Version,
			Content: string(content),
		})
	}

	stored, err := gorm.G[schemaMigrationFile](db).Where("version <= ?", latest).Find(ctx)
	if err != nil {
		return fmt.Errorf("failed to load stored migration files: %w", err)
	}
	if sameMigrationFiles(stored, files) {
		return nil
	}

	return db.Transaction(func(tx *gorm.DB) error {
		if _, err := gorm.G[schemaMigrationFile](tx).Where("version <= ?", latest).Delete(ctx); err != nil {
			return fmt.Errorf("failed to clear stored migration files: %w", err)
		}
		if err := gorm.G[schemaMigrationFile](tx).CreateInBatches(ctx, &files, 100); err != nil {
			return fmt.Errorf("failed to store migration files: %w", err)
		}
		return nil
	})
}

func sameMigrationFiles(stored, embedded []schemaMigrationFile) bool {
	if len(stored) != len(embedded) {
		return false
	}
	contents := make(map[string]string, len(stored))
	for _, file := range stored {
		contents[file.Name] = file.Content
	}
	for _, file := range embedded {
		if content, ok := contents[file.Name]; !ok || content != file.Content {
			return false
		}
	}
	return true
}

// rollBackNewerMigrations rolls the schema back from version to latest with the
// migration files the newer OrcaCD version stored. A backup is taken first.
func rollBackNewerMigrations(ctx context.Context, db *gorm.DB, version uint, dirty bool, latest uint) error {
	if dirty {
		return fmt.Errorf("database schema v%d is in a dirty migration state and cannot be rolled back", version)
	}
	if !AllowDowngrade {
		return fmt.Errorf(
			"database schema v%d was migrated by a newer OrcaCD version, this version supports up to schema v%d; start the hub with ALLOW_DOWNGRADE=true to roll it back",
			version, latest,
		)
	}

	stored, err := gorm.G[schemaMigrationFile](db).Where("version <= ?", version).Find(ctx)
	if err != nil {
		return fmt.Errorf("failed to load stored migration files: %w", err)
	}

	files := make(fstest.MapFS, len(stored))
	ups := make(map[uint]bool)
	downs := make(map[uint]bool)
	for _, file := range stored {
		files[file.Name] = &fstest.MapFile{Data: []byte(file.Content)}
		migration, err := source.Parse(file.Name)
		if err != nil {
			return fmt.Errorf("failed to parse stored migration file %q: %w", file.Name, err)
		}
		if migration.Direction == source.Up {
			ups[migration.Version] = true
		} else {
			downs[migration.Version] = true
		}
	}
	// Version numbers can have gaps, so check the newer migrations that were stored plus the applied one
	ups[version] = true
	for v := range ups {
		if v > latest && !downs[v] {
			return fmt.Errorf("migration %d cannot be rolled back because the OrcaCD version that applied it did not store its files", v)
		}
	}

	backupPath, err := backupBeforeDowngrade(ctx, db, version)
	if err != nil {
		return err
	}

	logger.Warn().
		Uint("from", version).
		Uint("to", latest).
		Str("backup", backupPath).
		Msg("Rolling back database migrations of a newer OrcaCD version")

	if err := migrateSchemaTo(db, files, latest); err != nil {
		return fmt.Errorf("failed to roll back database schema v%d to v%d (backup at %s): %w", version, latest, backupPath, err)
	}

	if _, err := gorm.G[schemaMigrationFile](db).Where("version > ?", latest).Delete(ctx); err != nil {
		return fmt.Errorf("failed to remove rolled back migration files: %w", err)
	}

	logger.Info().Uint("version", latest).Msg("Rolled back database schema")
	return nil
}

// newMigrator creates a migrator for the migration files in dir of fsys.
func newMigrator(sqlDB *sql.DB, fsys gofs.FS, dir string) (*migrate.Migrate, error) {
	driver, err := sqlite3.WithInstance(sqlDB, &sqlite3.Config{})
	if err != nil {
		return nil, err
	}

	src, err := iofs.New(fsys, dir)
	if err != nil {
		return nil, err
	}

	return migrate.NewWithInstance("iofs", src, "sqlite3", driver)
}

func migrateSchemaTo(db *gorm.DB, files fstest.MapFS, version uint) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}

	m, err := newMigrator(sqlDB, files, ".")
	if err != nil {
		return err
	}

	if err := m.Migrate(version); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	return nil
}

// backupBeforeDowngrade copies the database next to its file, as rolling back migrations can drop data.
func backupBeforeDowngrade(ctx context.Context, db *gorm.DB, version uint) (string, error) {
	main, err := gorm.G[databaseFile](db).Raw("SELECT file FROM pragma_database_list WHERE name = 'main'").First(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to determine database file: %w", err)
	}
	if main.File == "" {
		return "", errors.New("cannot back up an in-memory database before rolling it back")
	}

	backupPath := fmt.Sprintf("%s-downgrade-v%d-%d.bak", main.File, version, time.Now().Unix())
	if err := db.Exec("VACUUM INTO ?", backupPath).Error; err != nil {
		return "", fmt.Errorf("failed to back up database before rolling it back: %w", err)
	}
	return backupPath, nil
}
