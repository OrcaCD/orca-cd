package models

import (
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
)

// NormalizeName canonicalizes an application name for case- and
// whitespace-insensitive uniqueness checks. It is the single source of truth
// used by both the route handlers and the name-hash backfill.
func NormalizeName(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

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
	Name                     crypto.EncryptedString  `gorm:"type:text;not null"`
	NameHash                 string                  `gorm:"type:text;not null;default:''"`
	Icon                     string                  `gorm:"type:text;not null;default:box"`
	RepositoryId             string                  `gorm:"type:text;not null"`
	Repository               Repository              `gorm:"foreignKey:RepositoryId"`
	AgentId                  string                  `gorm:"type:text;not null"`
	Agent                    Agent                   `gorm:"foreignKey:AgentId"`
	SyncStatus               SyncStatus              `gorm:"type:text;not null"`
	HealthStatus             HealthStatus            `gorm:"type:text;not null"`
	Branch                   string                  `gorm:"type:text;not null"`
	Commit                   string                  `gorm:"type:text;not null"`
	CommitMessage            string                  `gorm:"type:text;not null"`
	LastSyncedAt             *time.Time              `gorm:"type:timestamp;"`
	LastSyncError            *string                 `gorm:"type:text;"`
	Path                     string                  `gorm:"type:text;not null"`
	ComposeFile              crypto.EncryptedString  `gorm:"type:text;not null"`
	PreviousComposeFile      crypto.EncryptedString  `gorm:"type:text;not null"`
	ImagePollEnabled         bool                    `gorm:"not null;default:false"`
	ImagePollIntervalSeconds int64                   `gorm:"not null;default:120"`
	ImagePollDeleteOldImages bool                    `gorm:"not null;default:false"`
	ImageWebhookSecret       *crypto.EncryptedString `gorm:"type:text;"`
	Notifications            []Notification          `gorm:"many2many:application_notifications;"`
}

func (Application) TableName() string {
	return "applications"
}
