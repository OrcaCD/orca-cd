package database

import (
	"fmt"
	"log"
	"os"

	"github.com/OrcaCD/orca-cd/internal/models"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func Connect() {
	var err error

	// Configure GORM
	config := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	}

	// Get database driver from environment (default: sqlite)
	dbDriver := os.Getenv("DB_DRIVER")
	if dbDriver == "" {
		dbDriver = "sqlite"
	}

	// Connect based on driver type
	switch dbDriver {
	case "postgres", "postgresql":
		// Get PostgreSQL connection string
		dsn := os.Getenv("DATABASE_URL")
		if dsn == "" {
			dsn = "host=localhost user=postgres password=postgres dbname=go_react_demo port=5432 sslmode=disable"
		}
		DB, err = gorm.Open(postgres.Open(dsn), config)
		if err != nil {
			log.Fatal("Failed to connect to PostgreSQL:", err)
		}
		fmt.Println("PostgreSQL database connected successfully")

	case "sqlite":
		// Get database path from environment or use default
		dbPath := os.Getenv("DB_PATH")
		if dbPath == "" {
			dbPath = "./data/app.db"
		}
		DB, err = gorm.Open(sqlite.Open(dbPath), config)
		if err != nil {
			log.Fatal("Failed to connect to SQLite:", err)
		}
		fmt.Println("SQLite database connected successfully")

	default:
		log.Fatalf("Unsupported database driver: %s (supported: sqlite, postgres)", dbDriver)
	}

	// Auto migrate models
	if err := DB.AutoMigrate(&models.Message{}); err != nil {
		log.Fatal("Failed to migrate database:", err)
	}

	// Seed initial data if database is empty
	seedData()
}

func seedData() {
	var count int64
	DB.Model(&models.Message{}).Count(&count)

	if count == 0 {
		initialMessages := []models.Message{
			{Text: "Hello from Go backend!"},
			{Text: "This is a demo application"},
		}

		DB.Create(&initialMessages)
		fmt.Println("Database seeded with initial data")
	}
}

func GetDB() *gorm.DB {
	return DB
}
