package models

import (
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
)

type RepositoryProvider string

const (
	GitHub      RepositoryProvider = "github"
	GitLab      RepositoryProvider = "gitlab"
	Generic     RepositoryProvider = "generic"
	Bitbucket   RepositoryProvider = "bitbucket"
	AzureDevOps RepositoryProvider = "azure_devops"
	Gitea       RepositoryProvider = "gitea"
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
	// TODO add "scheduled" and maybe "github-app"
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
	Url             string                  `gorm:"type:text;not null;uniqueIndex:idx_repositories_url_sync_type"`
	Provider        RepositoryProvider      `gorm:"type:text;not null"`
	AuthMethod      RepositoryAuthMethod    `gorm:"type:text;not null"`
	AuthUser        *crypto.EncryptedString `gorm:"type:text;"`
	AuthToken       *crypto.EncryptedString `gorm:"type:text;"`
	SyncType        RepositorySyncType      `gorm:"type:text;not null;uniqueIndex:idx_repositories_url_sync_type"`
	SyncStatus      RepositorySyncStatus    `gorm:"type:text;not null"`
	LastSyncError   *string                 `gorm:"type:text;"`
	PollingInterval *time.Duration          `gorm:"type:integer;"`
	WebhookSecret   *crypto.EncryptedString `gorm:"type:text;"`
	LastSyncedAt    *time.Time              `gorm:"type:timestamp;"`
	CreatedBy       string                  `gorm:"type:text;not null;"`
	Applications    []Application           `gorm:"foreignKey:RepositoryId;"`
}

func (Repository) TableName() string {
	return "repositories"
}
