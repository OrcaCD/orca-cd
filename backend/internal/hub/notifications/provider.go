package notifications

import (
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	notificationprovider "github.com/OrcaCD/orca-cd/internal/hub/notifications/provider"
)

type Provider = notificationprovider.Provider

func Register(notificationType models.NotificationType, provider Provider) {
	notificationprovider.Register(notificationType, provider)
}

func Get(notificationType models.NotificationType) (Provider, error) {
	return notificationprovider.Get(notificationType)
}

func BuildShouterrrUrls(notificationType models.NotificationType, rawConfig string) ([]string, error) {
	provider, err := Get(notificationType)
	if err != nil {
		return nil, err
	}

	return provider.BuildShouterrrUrls(rawConfig)
}

// BuildShoutrrrURLs remains for backward compatibility with existing call sites.
func BuildShoutrrrURLs(notificationType models.NotificationType, rawConfig string) ([]string, error) {
	return BuildShouterrrUrls(notificationType, rawConfig)
}
