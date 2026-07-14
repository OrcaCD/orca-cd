package applications

import (
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	hubws "github.com/OrcaCD/orca-cd/internal/hub/websocket"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

func seedImagePullApp(t *testing.T, agentID string) *models.Application {
	t.Helper()
	repo := seedRepo(t)
	app := seedApp(t, repo.Id, agentID, "services: {}\n")
	return &app
}

func TestTriggerImagePull_AgentConnected_SendsRequest(t *testing.T) {
	setupTestDB(t)
	log := zerolog.Nop()
	hub := hubws.NewHub(&log)
	hubws.DefaultHub = hub
	t.Cleanup(func() { hubws.DefaultHub = nil })

	const agentID = "agent-pull-test"
	client, err := hub.Register(agentID, nil)
	if err != nil {
		t.Fatalf("failed to register agent: %v", err)
	}

	app := &models.Application{}
	app.Id = "app-pull-test"
	app.AgentId = agentID
	app.Name = crypto.EncryptedString("my-app")

	if !TriggerImagePull(app, models.ApplicationEventSourceImageWebhook) {
		t.Error("expected TriggerImagePull to return true for connected agent")
	}

	select {
	case msg := <-client.Send:
		req := msg.GetPullImagesRequest()
		if req == nil {
			t.Fatalf("expected PullImagesRequest payload, got %T", msg.Payload)
		}
		if req.ApplicationId != app.Id {
			t.Errorf("application_id: got %q, want %q", req.ApplicationId, app.Id)
		}
		if req.ApplicationName != "my-app" {
			t.Errorf("application_name: got %q, want %q", req.ApplicationName, "my-app")
		}
		if req.RequestId == "" {
			t.Error("expected non-empty request_id")
		}

		// The dispatched request ID correlates with a running image_update event.
		event, err := gorm.G[models.ApplicationEvent](db.DB).
			Where("request_id = ?", req.RequestId).First(t.Context())
		if err != nil {
			t.Fatalf("failed to load image update event: %v", err)
		}
		if event.Type != models.ApplicationEventImageUpdate ||
			event.Source != models.ApplicationEventSourceImageWebhook ||
			event.Status != models.ApplicationEventRunning {
			t.Fatalf("unexpected image update event: %+v", event)
		}
	default:
		t.Error("expected a message to be queued on the agent's Send channel")
	}
}

func TestTriggerImagePull_AgentNotConnected_ReturnsFalseAndFailsEvent(t *testing.T) {
	setupTestDB(t)
	log := zerolog.Nop()
	hub := hubws.NewHub(&log)
	hubws.DefaultHub = hub
	t.Cleanup(func() { hubws.DefaultHub = nil })

	agent := seedAgent(t)
	app := seedImagePullApp(t, agent.Id)

	if TriggerImagePull(app, models.ApplicationEventSourceGitHubActions) {
		t.Error("expected TriggerImagePull to return false for disconnected agent")
	}

	events, err := gorm.G[models.ApplicationEvent](db.DB).
		Where("application_id = ?", app.Id).Find(t.Context())
	if err != nil {
		t.Fatalf("failed to load events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	event := events[0]
	if event.Type != models.ApplicationEventImageUpdate ||
		event.Source != models.ApplicationEventSourceGitHubActions ||
		event.Status != models.ApplicationEventFailed ||
		event.ErrorMessage == nil {
		t.Fatalf("expected failed github_actions image event with error, got %+v", event)
	}
}

func TestTriggerImagePull_HubNil_ReturnsFalseAndFailsEvent(t *testing.T) {
	setupTestDB(t)
	hubws.DefaultHub = nil

	agent := seedAgent(t)
	app := seedImagePullApp(t, agent.Id)

	if TriggerImagePull(app, models.ApplicationEventSourceImageWebhook) {
		t.Error("expected TriggerImagePull to return false when hub is nil")
	}

	event, err := gorm.G[models.ApplicationEvent](db.DB).
		Where("application_id = ?", app.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load event: %v", err)
	}
	if event.Status != models.ApplicationEventFailed {
		t.Fatalf("expected failed event when hub is nil, got %+v", event)
	}
}
