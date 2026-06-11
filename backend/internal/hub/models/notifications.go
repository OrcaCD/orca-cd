package models

import "github.com/OrcaCD/orca-cd/internal/hub/crypto"

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
	NotificationTypeWebhook NotificationType = "webhook"
	NotificationTypeCustom  NotificationType = "custom"
)

type Notification struct {
	Base
	Name            crypto.EncryptedString `gorm:"type:text;not null"`
	Enabled         bool                   `gorm:"not null"`
	EnableByDefault bool                   `gorm:"not null"`
	Status          NotificationStatus     `gorm:"type:text;not null"`
	Type            NotificationType       `gorm:"type:text;not null"`
	Config          crypto.EncryptedString `gorm:"type:text;not null"`
	Applications    []Application          `gorm:"many2many:application_notifications;"`
}

func (Notification) TableName() string {
	return "notifications"
}
