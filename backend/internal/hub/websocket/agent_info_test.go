package websocket

import (
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

func loadAgent(t *testing.T, id string) models.Agent {
	t.Helper()
	agent, err := gorm.G[models.Agent](db.DB).Where("id = ?", id).First(t.Context())
	if err != nil {
		t.Fatalf("load agent %s: %v", id, err)
	}
	return agent
}

func TestHandleAgentInfo_StoresVersions(t *testing.T) {
	setupHandlerTestEnv(t)
	agent := createTestAgent(t, "key-1")
	other := createTestAgent(t, "key-2")

	nop := zerolog.Nop()
	handleAgentInfo(t.Context(), &Client{Id: agent.Id}, &messages.AgentInfo{
		Version:       " v0.5.0 ",
		DockerVersion: "28.1.1",
	}, &nop)

	got := loadAgent(t, agent.Id)
	if got.Version != "v0.5.0" {
		t.Errorf("expected version %q, got %q", "v0.5.0", got.Version)
	}
	if got.DockerVersion != "28.1.1" {
		t.Errorf("expected docker version %q, got %q", "28.1.1", got.DockerVersion)
	}
	if got.Name.String() != "test-agent" {
		t.Errorf("agent name must be untouched, got %q", got.Name.String())
	}

	if untouched := loadAgent(t, other.Id); untouched.Version != "" || untouched.DockerVersion != "" {
		t.Errorf("other agent must be untouched, got %q / %q", untouched.Version, untouched.DockerVersion)
	}
}

func TestHandleAgentInfo_ClearsMissingDockerVersion(t *testing.T) {
	setupHandlerTestEnv(t)
	agent := createTestAgent(t, "key-1")
	nop := zerolog.Nop()
	client := &Client{Id: agent.Id}

	handleAgentInfo(t.Context(), client, &messages.AgentInfo{Version: "v0.5.0", DockerVersion: "28.1.1"}, &nop)
	handleAgentInfo(t.Context(), client, &messages.AgentInfo{Version: "v0.6.0"}, &nop)

	got := loadAgent(t, agent.Id)
	if got.Version != "v0.6.0" {
		t.Errorf("expected version %q, got %q", "v0.6.0", got.Version)
	}
	if got.DockerVersion != "" {
		t.Errorf("expected docker version to be cleared, got %q", got.DockerVersion)
	}
}

func TestHandleAgentInfo_TruncatesLongVersions(t *testing.T) {
	setupHandlerTestEnv(t)
	agent := createTestAgent(t, "key-1")
	nop := zerolog.Nop()

	handleAgentInfo(t.Context(), &Client{Id: agent.Id}, &messages.AgentInfo{
		Version: strings.Repeat("a", maxReportedVersionLength*2),
	}, &nop)

	if got := loadAgent(t, agent.Id); len(got.Version) != maxReportedVersionLength {
		t.Errorf("expected version length %d, got %d", maxReportedVersionLength, len(got.Version))
	}
}

func TestHandleAgentInfo_NilIsIgnored(t *testing.T) {
	setupHandlerTestEnv(t)
	agent := createTestAgent(t, "key-1")
	nop := zerolog.Nop()

	handleAgentInfo(t.Context(), &Client{Id: agent.Id}, nil, &nop)

	if got := loadAgent(t, agent.Id); got.Version != "" {
		t.Errorf("expected empty version, got %q", got.Version)
	}
}
