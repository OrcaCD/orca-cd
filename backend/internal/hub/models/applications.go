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
	Stopped       HealthStatus = "stopped"
	UnknownHealth HealthStatus = "unknown"
	// Degraded means some services of the application are healthy while
	// others are unhealthy or stopped.
	Degraded HealthStatus = "degraded"
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
	LastSyncSource           *ApplicationEventSource `gorm:"type:text;"`
	Path                     string                  `gorm:"type:text;not null"`
	ComposeFile              crypto.EncryptedString  `gorm:"type:text;not null"`
	PreviousComposeFile      crypto.EncryptedString  `gorm:"type:text;not null"`
	ImagePollEnabled         bool                    `gorm:"not null;default:false"`
	ImagePollIntervalSeconds int64                   `gorm:"not null;default:120"`
	DeleteOldImages          bool                    `gorm:"not null;default:false"`
	ImageWebhookSecret       *crypto.EncryptedString `gorm:"type:text;"`
	// CurrentDeploymentId is the deployment the agent runs, if it is known.
	CurrentDeploymentId *string `gorm:"type:text;"`
	// RolledBackAt is set while the application runs an earlier deployment it
	// was rolled back to, which drifts from its commit in Git.
	RolledBackAt *time.Time `gorm:"type:timestamp;"`
	// RolledBackCommit is the commit of the deployment it was rolled back to.
	RolledBackCommit *string `gorm:"type:text;"`
	// AutoSyncPaused stops automatic syncs of a rolled back application until
	// they are resumed or the application is deployed again.
	AutoSyncPaused bool `gorm:"not null;default:false"`
	// Automatic rollback to the previous deployment, off by default since it
	// diverges from the state in Git.
	RollbackEnabled         bool `gorm:"not null;default:false"`
	RollbackOnDeployFailure bool `gorm:"not null;default:true"`
	// RollbackOnUnhealthy rolls back deployments that become unhealthy, or
	// don't become healthy, within RollbackHealthTimeoutSeconds.
	RollbackOnUnhealthy          bool           `gorm:"not null;default:true"`
	RollbackHealthTimeoutSeconds int64          `gorm:"not null;default:300"`
	RollbackPauseAutoSync        bool           `gorm:"not null;default:true"`
	Notifications                []Notification `gorm:"many2many:application_notifications;"`
}

// HealthGate returns how long a new deployment is watched before it counts as
// succeeded, or 0 when its health doesn't decide whether to roll back.
func (a *Application) HealthGate() time.Duration {
	if !a.RollbackEnabled || !a.RollbackOnUnhealthy {
		return 0
	}
	return time.Duration(a.RollbackHealthTimeoutSeconds) * time.Second
}

func (Application) TableName() string {
	return "applications"
}
