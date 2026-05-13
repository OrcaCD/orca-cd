package provider

import (
	"slices"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
)

type staticProvider struct {
	urls []string
}

func (p staticProvider) BuildShouterrrUrls(string) ([]string, error) {
	return p.urls, nil
}

func cloneProviderRegistry(src map[models.NotificationType]Provider) map[models.NotificationType]Provider {
	cloned := make(map[models.NotificationType]Provider, len(src))
	for key, value := range src {
		cloned[key] = value
	}

	return cloned
}

func TestGetDefaultProvider(t *testing.T) {
	provider, err := Get(models.NotificationTypeDiscord)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if provider == nil {
		t.Fatal("expected default discord provider to be registered")
	}
}

func TestRegisterAndGet(t *testing.T) {
	originalRegistry := cloneProviderRegistry(registry)
	t.Cleanup(func() {
		registry = cloneProviderRegistry(originalRegistry)
	})

	registry = map[models.NotificationType]Provider{}

	notificationType := models.NotificationType("provider-register-test")
	expected := []string{"test://a", "test://b"}

	Register(notificationType, staticProvider{urls: expected})

	gotProvider, err := Get(notificationType)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	gotURLs, err := gotProvider.BuildShouterrrUrls("ignored")
	if err != nil {
		t.Fatalf("BuildShouterrrUrls() error = %v", err)
	}
	if !slices.Equal(gotURLs, expected) {
		t.Fatalf("expected %v, got %v", expected, gotURLs)
	}
}

func TestGetUnregisteredProvider(t *testing.T) {
	_, err := Get(models.NotificationType("definitely-missing-provider"))
	if err == nil {
		t.Fatal("expected error for missing provider")
	}
	if !strings.Contains(err.Error(), "no provider registered") {
		t.Fatalf("unexpected error: %v", err)
	}
}
