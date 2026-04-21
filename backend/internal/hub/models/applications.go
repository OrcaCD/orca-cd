package models

import (
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
)

type SyncStatus string

const (
	Synced      SyncStatus = "synced"
	OutOfSync   SyncStatus = "out_of_sync"
	Syncing     SyncStatus = "syncing"
	UnknownSync SyncStatus = "unknown"
)

type HealthStatus string

const (
	Healthy       HealthStatus = "healthy"
	Unhealthy     HealthStatus = "unhealthy"
	UnknownHealth HealthStatus = "unknown"
)

type Application struct {
	Base
	Name          crypto.EncryptedString `gorm:"type:text;not null"`
	RepositoryId  string                 `gorm:"type:text;not null"`
	Repository    Repository             `gorm:"foreignKey:RepositoryId"`
	AgentId       string                 `gorm:"type:text;not null"`
	Agent         Agent                  `gorm:"foreignKey:AgentId"`
	SyncStatus    SyncStatus             `gorm:"type:text;not null"`
	HealthStatus  HealthStatus           `gorm:"type:text;not null"`
	Branch        string                 `gorm:"type:text;not null"`
	Commit        string                 `gorm:"type:text;not null"`
	CommitMessage string                 `gorm:"type:text;not null"`
	LastSyncedAt  *time.Time             `gorm:"type:timestamp;"`
	Path          string                 `gorm:"type:text;not null"`
	ComposeFile   crypto.EncryptedString `gorm:"type:text;not null"`
}

func (Application) TableName() string {
	return "applications"
}
