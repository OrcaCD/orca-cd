package applications

import (
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	hubws "github.com/OrcaCD/orca-cd/internal/hub/websocket"
	"github.com/rs/zerolog"
)

func TestTriggerImagePull_AgentConnected_SendsRequest(t *testing.T) {
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

	if !TriggerImagePull(app) {
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
	default:
		t.Error("expected a message to be queued on the agent's Send channel")
	}
}

func TestTriggerImagePull_AgentNotConnected_ReturnsFalse(t *testing.T) {
	log := zerolog.Nop()
	hub := hubws.NewHub(&log)
	hubws.DefaultHub = hub
	t.Cleanup(func() { hubws.DefaultHub = nil })

	app := &models.Application{}
	app.AgentId = "non-existent-agent"
	app.Name = crypto.EncryptedString("my-app")

	if TriggerImagePull(app) {
		t.Error("expected TriggerImagePull to return false for disconnected agent")
	}
}

func TestTriggerImagePull_HubNil_ReturnsFalse(t *testing.T) {
	hubws.DefaultHub = nil

	app := &models.Application{}
	app.AgentId = "agent-1"
	app.Name = crypto.EncryptedString("my-app")

	if TriggerImagePull(app) {
		t.Error("expected TriggerImagePull to return false when hub is nil")
	}
}
