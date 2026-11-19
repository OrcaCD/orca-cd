package sse

import (
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
)

func TestTopicApplicationImageUpdates(t *testing.T) {
	topic := TopicApplicationImageUpdates("application-1")
	if got, want := topic.URL(), "/api/v1/applications/application-1/image-updates"; got != want {
		t.Errorf("URL() = %q, want %q", got, want)
	}
	// Only users who can read the application get its image updates
	if resource, id := topic.Resource(); resource != models.ResourceApplication || id != "application-1" {
		t.Errorf("Resource() = %q, %q", resource, id)
	}
}

func TestTopicSettings_SentToEveryone(t *testing.T) {
	if got, want := TopicSettings.URL(), "/api/v1/settings"; got != want {
		t.Errorf("URL() = %q, want %q", got, want)
	}
	if resource, _ := TopicSettings.Resource(); resource != "" || TopicSettings.AdminOnly() || !TopicSettings.SentTo("user-1") {
		t.Error("expected the settings to be sent to every user")
	}
}
