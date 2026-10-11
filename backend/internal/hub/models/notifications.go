package models

import (
	"slices"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
)

type NotificationStatus string

const (
	NotificationStatusUnknown NotificationStatus = "unknown"
	NotificationStatusSuccess NotificationStatus = "success"
	NotificationStatusError   NotificationStatus = "error"
)

type NotificationType string

const (
	NotificationTypeDiscord NotificationType = "discord"
	NotificationTypeGotify  NotificationType = "gotify"
	NotificationTypeSlack   NotificationType = "slack"
	NotificationTypeEmail   NotificationType = "email"
	NotificationTypeTeams   NotificationType = "teams"
	NotificationTypeWebhook NotificationType = "webhook"
	NotificationTypeCustom  NotificationType = "custom"
)

// NotificationEvent identifies an occurrence a notification can subscribe to.
type NotificationEvent string

const (
	NotificationEventDeploymentSucceeded  NotificationEvent = "application.deployment.succeeded"
	NotificationEventDeploymentFailed     NotificationEvent = "application.deployment.failed"
	NotificationEventImageUpdateSucceeded NotificationEvent = "application.image_update.succeeded"
	NotificationEventImageUpdateFailed    NotificationEvent = "application.image_update.failed"
	NotificationEventSyncFailed           NotificationEvent = "application.sync.failed"
	NotificationEventHealthUnhealthy      NotificationEvent = "application.health.unhealthy"
	NotificationEventHealthRecovered      NotificationEvent = "application.health.recovered"

	NotificationEventAgentOffline NotificationEvent = "agent.offline"
	NotificationEventAgentOnline  NotificationEvent = "agent.online"

	NotificationEventRepositorySyncFailed    NotificationEvent = "repository.sync.failed"
	NotificationEventRepositorySyncRecovered NotificationEvent = "repository.sync.recovered"
)

// NotificationEvents lists every supported event in display order.
var NotificationEvents = []NotificationEvent{
	NotificationEventDeploymentSucceeded,
	NotificationEventDeploymentFailed,
	NotificationEventImageUpdateSucceeded,
	NotificationEventImageUpdateFailed,
	NotificationEventSyncFailed,
	NotificationEventHealthUnhealthy,
	NotificationEventHealthRecovered,
	NotificationEventAgentOffline,
	NotificationEventAgentOnline,
	NotificationEventRepositorySyncFailed,
	NotificationEventRepositorySyncRecovered,
}

func (e NotificationEvent) IsValid() bool {
	return slices.Contains(NotificationEvents, e)
}

type Notification struct {
	Base
	Name            crypto.EncryptedString `gorm:"type:text;not null"`
	Enabled         bool                   `gorm:"not null"`
	AllApplications bool                   `gorm:"not null"`
	AllAgents       bool                   `gorm:"not null"`
	AllRepositories bool                   `gorm:"not null"`
	Events          []NotificationEvent    `gorm:"type:text;not null;serializer:json"`
	Status          NotificationStatus     `gorm:"type:text;not null"`
	Type            NotificationType       `gorm:"type:text;not null"`
	Config          crypto.EncryptedString `gorm:"type:text;not null"`
	Applications    []Application          `gorm:"many2many:application_notifications;"`
	Agents          []Agent                `gorm:"many2many:agent_notifications;"`
	Repositories    []Repository           `gorm:"many2many:repository_notifications;"`
}

func (n *Notification) SubscribesTo(event NotificationEvent) bool {
	return slices.Contains(n.Events, event)
}

func (Notification) TableName() string {
	return "notifications"
}
