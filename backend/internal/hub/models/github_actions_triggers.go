package models

type GitHubActionsTrigger struct {
	Base
	RepositoryId   string   `gorm:"type:text;not null"`
	Repository     string   `gorm:"type:text;not null"`
	RunId          string   `gorm:"type:text;not null"`
	SyncRepo       bool     `gorm:"not null"`
	PullImages     bool     `gorm:"not null"`
	ApplicationIds []string `gorm:"type:text;not null;serializer:json"`
}

func (GitHubActionsTrigger) TableName() string { return "github_actions_triggers" }

// GitHubActionsTriggerSkip is an operation skipped before any application
// history event was created.
type GitHubActionsTriggerSkip struct {
	TriggerId     string               `gorm:"type:text;primaryKey"`
	ApplicationId string               `gorm:"type:text;primaryKey"`
	Type          ApplicationEventType `gorm:"type:text;primaryKey"`
	Reason        string               `gorm:"type:text;not null"`
}

func (GitHubActionsTriggerSkip) TableName() string { return "github_actions_trigger_skips" }
