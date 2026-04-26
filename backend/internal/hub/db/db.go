package db

import (
	"embed"
	"net/url"
	"os"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

var DB *gorm.DB
var logger zerolog.Logger

const sqliteFilePath = "data/hub.db"

func sqliteDSN(readOnly bool) string {
	q := url.Values{}
	// https://sqlite.org/pragma.html#pragma_busy_timeout
	q.Set("_busy_timeout", "5000") // wait up to 5 seconds if the database is locked

	// https://sqlite.org/pragma.html#pragma_foreign_keys
	q.Set("_foreign_keys", "ON") // enable foreign key constraints

	// https://sqlite.org/pragma.html#pragma_journal_mode
	q.Set("_journal_mode", "WAL") // allows concurrent reads during writes

	// https://sqlite.org/pragma.html#pragma_synchronous
	q.Set("_synchronous", "NORMAL") // safe durability guarantee with WAL

	// https://sqlite.org/pragma.html#pragma_auto_vacuum
	q.Set("_auto_vacuum", "2") // collect data for running incremental_vacuum to prevent database file from growing indefinitely

	// https://sqlite.org/pragma.html#pragma_cache_size
	q.Set("_cache_size", "-12000") // 12 MB page cache; negative value = kibibytes

	if readOnly {
		// https://www.sqlite.org/uri.html
		q.Set("mode", "ro") // Read-only in demo mode
	}
	return sqliteFilePath + "?" + q.Encode()
}

func configureSQLitePool(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}

	// SQLite allows a single writer; limiting the pool to one connection avoids
	// internal lock contention from multiple pooled connections.
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(0)

	return nil
}

func Connect(newLogger zerolog.Logger, logLevel zerolog.Level, demo bool) error {
	logger = newLogger
	if err := os.MkdirAll("data", 0750); err != nil {
		return err
	}

	gormLogLevel := gormlogger.Error
	if logLevel <= zerolog.DebugLevel {
		gormLogLevel = gormlogger.Info
	}

	gormConfig := &gorm.Config{
		Logger: NewGormLogger(logger, GormLoggerConfig{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  gormLogLevel,
			IgnoreRecordNotFoundError: true,
		}),
	}

	db, err := gorm.Open(sqlite.Open(sqliteDSN(false)), gormConfig)
	if err != nil {
		return err
	}

	if err := configureSQLitePool(db); err != nil {
		return err
	}

	if err := runMigrations(db); err != nil {
		return err
	}

	if demo {
		if err := seedDemoData(db); err != nil {
			return err
		}

		sqlDB, err := db.DB()
		if err != nil {
			return err
		}
		if err := sqlDB.Close(); err != nil {
			return err
		}

		readOnlyDB, err := gorm.Open(sqlite.Open(sqliteDSN(true)), gormConfig)
		if err != nil {
			return err
		}

		if err := configureSQLitePool(readOnlyDB); err != nil {
			return err
		}

		db = readOnlyDB
	}

	DB = db
	return nil
}

func runMigrations(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}

	driver, err := sqlite3.WithInstance(sqlDB, &sqlite3.Config{})
	if err != nil {
		return err
	}

	src, err := iofs.New(migrationFiles, "migrations")
	if err != nil {
		return err
	}

	m, err := migrate.NewWithInstance("iofs", src, "sqlite3", driver)
	if err != nil {
		return err
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}

	return nil
}

func StartVacuumScheduler() (stop func()) {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := IncrementalVacuum(); err != nil {
					logger.Error().Err(err).Msg("incremental vacuum failed")
				}
			case <-done:
				return
			}
		}
	}()
	return func() { close(done) }
}

func IncrementalVacuum() error {
	logger.Debug().Msg("Running PRAGMA incremental_vacuum(200) and wal_checkpoint(PASSIVE)")
	if err := DB.Exec("PRAGMA incremental_vacuum(200)").Error; err != nil {
		return err
	}
	if err := DB.Exec("PRAGMA wal_checkpoint(PASSIVE)").Error; err != nil {
		return err
	}
	return nil
}
