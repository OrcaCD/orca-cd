package db

import (
	"embed"
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

func Connect(logger zerolog.Logger, logLevel zerolog.Level, demo bool) error {
	if err := os.MkdirAll("data", 0750); err != nil {
		return err
	}

	gormLogLevel := gormlogger.Error
	if logLevel <= zerolog.DebugLevel {
		gormLogLevel = gormlogger.Info
	}

	dbPath := "data/hub.db"
	gormConfig := &gorm.Config{
		Logger: NewGormLogger(logger, GormLoggerConfig{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  gormLogLevel,
			IgnoreRecordNotFoundError: true,
		}),
	}

	db, err := gorm.Open(sqlite.Open(dbPath), gormConfig)
	if err != nil {
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

		readOnlyDB, err := gorm.Open(sqlite.Open(dbPath+"?mode=ro"), gormConfig)
		if err != nil {
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
