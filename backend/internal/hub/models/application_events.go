package models

import "time"

type ApplicationEventType string

const (
	ApplicationEventDeployment  ApplicationEventType = "deployment"
	ApplicationEventCommitSync  ApplicationEventType = "commit_sync"
	ApplicationEventImageUpdate ApplicationEventType = "image_update"
)

type ApplicationEventSource string

const (
	ApplicationEventSourceManual             ApplicationEventSource = "manual"
	ApplicationEventSourceApplicationCreated ApplicationEventSource = "application_created"
	ApplicationEventSourceRepositoryPolling  ApplicationEventSource = "repository_polling"
	ApplicationEventSourceRepositoryWebhook  ApplicationEventSource = "repository_webhook"
	ApplicationEventSourceGitHubActions      ApplicationEventSource = "github_actions"
	ApplicationEventSourceImagePolling       ApplicationEventSource = "image_polling"
	ApplicationEventSourceImageWebhook       ApplicationEventSource = "image_webhook"
)

type ApplicationEventStatus string

const (
	ApplicationEventRunning   ApplicationEventStatus = "running"
	ApplicationEventSucceeded ApplicationEventStatus = "succeeded"
	ApplicationEventFailed    ApplicationEventStatus = "failed"
	ApplicationEventNoChange  ApplicationEventStatus = "no_change"
)

type ApplicationEvent struct {
	Base
	ApplicationId string                 `gorm:"type:text;not null;index:idx_application_events_app_created,priority:1"`
	Application   Application            `gorm:"foreignKey:ApplicationId;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`
	RequestId     *string                `gorm:"type:text;uniqueIndex"`
	Type          ApplicationEventType   `gorm:"type:text;not null"`
	Source        ApplicationEventSource `gorm:"type:text;not null"`
	Status        ApplicationEventStatus `gorm:"type:text;not null"`
	ActorUserId   *string                `gorm:"type:text;index"`
	Actor         *User                  `gorm:"foreignKey:ActorUserId;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;"`
	ActorName     *string                `gorm:"type:text"`
	CommitHash    *string                `gorm:"type:text"`
	CommitMessage *string                `gorm:"type:text"`
	ErrorMessage  *string                `gorm:"type:text"`
	CompletedAt   *time.Time             `gorm:"type:timestamp"`
}

func (ApplicationEvent) TableName() string { return "application_events" }
