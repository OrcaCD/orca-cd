package models

import (
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
)

type RepositoryProvider string

const (
	GitHub  RepositoryProvider = "github"
	GitLab  RepositoryProvider = "gitlab"
	Generic RepositoryProvider = "generic"
	// Todo: Add more providers like Bitbucket, Azure DevOps, etc.
)

type RepositoryAuthMethod string

const (
	AuthMethodNone  RepositoryAuthMethod = "none"
	AuthMethodToken RepositoryAuthMethod = "token"
	AuthMethodBasic RepositoryAuthMethod = "basic"
	AuthMethodSSH   RepositoryAuthMethod = "ssh"
)

type RepositorySyncType string

const (
	SyncTypePolling RepositorySyncType = "polling"
	SyncTypeWebhook RepositorySyncType = "webhook"
	SyncTypeManual  RepositorySyncType = "manual"
	// Maybe later "scheduled" or "github-app"
)

type RepositorySyncStatus string

const (
	SyncStatusUnknown RepositorySyncStatus = "unknown"
	SyncStatusSyncing RepositorySyncStatus = "syncing"
	SyncStatusFailed  RepositorySyncStatus = "failed"
	SyncStatusSuccess RepositorySyncStatus = "success"
)

type Repository struct {
	Base
	Name            string                  `gorm:"type:text;not null"`
	Url             string                  `gorm:"type:text;not null"`
	Provider        RepositoryProvider      `gorm:"type:text;not null"`
	AuthMethod      RepositoryAuthMethod    `gorm:"type:text;not null"`
	AuthUser        *crypto.EncryptedString `gorm:"type:text;"`
	AuthToken       *crypto.EncryptedString `gorm:"type:text;"`
	SyncType        RepositorySyncType      `gorm:"type:text;not null"`
	SyncStatus      RepositorySyncStatus    `gorm:"type:text;not null"`
	LastSyncError   *string                 `gorm:"type:text;"`
	PollingInterval *time.Duration          `gorm:"type:integer;"`
	WebhookSecret   *crypto.EncryptedString `gorm:"type:text;"`
	LastSyncedAt    *time.Time              `gorm:"type:timestamp;"`
	CreatedBy       string                  `gorm:"type:text;not null;"`
}

func (Repository) TableName() string {
	return "repositories"
}
