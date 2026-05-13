package notifications

import (
	"slices"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
)

type wrapperStaticProvider struct {
	urls []string
}

func (p wrapperStaticProvider) BuildShouterrrUrls(string) ([]string, error) {
	return p.urls, nil
}

func TestBuildShoutrrrURLsBackwardCompatibility(t *testing.T) {
	rawConfig := "discord://token@channel"

	newURLs, err := BuildShouterrrUrls(models.NotificationTypeDiscord, rawConfig)
	if err != nil {
		t.Fatalf("BuildShouterrrUrls() error = %v", err)
	}

	legacyURLs, err := BuildShoutrrrURLs(models.NotificationTypeDiscord, rawConfig)
	if err != nil {
		t.Fatalf("BuildShoutrrrURLs() error = %v", err)
	}

	if !slices.Equal(newURLs, legacyURLs) {
		t.Fatalf("expected legacy and new API to return same URLs, got %v and %v", newURLs, legacyURLs)
	}
}

func TestRegisterAndGetWrapper(t *testing.T) {
	customType := models.NotificationType("wrapper-provider-test")
	expected := []string{"test://wrapped"}
	Register(customType, wrapperStaticProvider{urls: expected})

	gotProvider, err := Get(customType)
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

	builtURLs, err := BuildShouterrrUrls(customType, "ignored")
	if err != nil {
		t.Fatalf("BuildShouterrrUrls() wrapper error = %v", err)
	}
	if !slices.Equal(builtURLs, expected) {
		t.Fatalf("expected %v, got %v", expected, builtURLs)
	}
}