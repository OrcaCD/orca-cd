package provider

import (
	"fmt"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
)

type Provider interface {
	BuildShoutrrrUrls(rawConfig string) ([]string, error)
}

var registry = map[models.NotificationType]Provider{}

func init() {
	Register(models.NotificationTypeDiscord, DiscordProvider{})
	Register(models.NotificationTypeGotify, GotifyProvider{})
	Register(models.NotificationTypeSlack, SlackProvider{})
	Register(models.NotificationTypeEmail, EmailProvider{})
	Register(models.NotificationTypeWebhook, WebhookProvider{})
	Register(models.NotificationTypeCustom, CustomProvider{})
}

func Register(notificationType models.NotificationType, provider Provider) {
	registry[notificationType] = provider
}

func Get(notificationType models.NotificationType) (Provider, error) {
	provider, ok := registry[notificationType]
	if !ok {
		return nil, fmt.Errorf("no provider registered for notification type %q", notificationType)
	}
	return provider, nil
}

func BuildShoutrrrUrls(notificationType models.NotificationType, rawConfig string) ([]string, error) {
	provider, err := Get(notificationType)
	if err != nil {
		return nil, err
	}

	return provider.BuildShoutrrrUrls(rawConfig)
}
