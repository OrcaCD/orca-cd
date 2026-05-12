package provider

import (
	"fmt"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
)

type Provider interface {
	BuildShouterrrUrls(rawConfig string) ([]string, error)
}

var registry = map[models.NotificationType]Provider{}

func init() {
	Register(models.NotificationTypeDiscord, DiscordProvider{})
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
