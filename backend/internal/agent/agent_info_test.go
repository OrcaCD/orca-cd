package agent

import (
	"testing"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/version"
)

type stubDockerState struct {
	ready   bool
	version string
}

func (s stubDockerState) Ready() bool {
	return s.ready
}

func (s stubDockerState) ServerVersion() string {
	return s.version
}

func receiveAgentInfo(t *testing.T, sender *stubSender) *messages.AgentInfo {
	t.Helper()
	select {
	case msg := <-sender.sent:
		info := msg.GetAgentInfo()
		if info == nil {
			t.Fatalf("expected AgentInfo, got %T", msg.Payload)
		}
		return info
	default:
		t.Fatal("expected a message to be sent")
		return nil
	}
}

func TestSendAgentInfo_IncludesVersions(t *testing.T) {
	sender := &stubSender{sent: make(chan *messages.ClientMessage, 1)}

	sendAgentInfo(sender, stubDockerState{ready: true, version: "28.1.1"})

	info := receiveAgentInfo(t, sender)
	if info.Version != version.Version {
		t.Errorf("expected version %q, got %q", version.Version, info.Version)
	}
	if info.DockerVersion != "28.1.1" {
		t.Errorf("expected docker version %q, got %q", "28.1.1", info.DockerVersion)
	}
	if info.DockerUnreachable {
		t.Error("expected docker to be reported as reachable")
	}
}

func TestSendAgentInfo_DockerUnavailable(t *testing.T) {
	sender := &stubSender{sent: make(chan *messages.ClientMessage, 1)}

	sendAgentInfo(sender, stubDockerState{})

	info := receiveAgentInfo(t, sender)
	if info.Version != version.Version {
		t.Errorf("expected version %q, got %q", version.Version, info.Version)
	}
	if info.DockerVersion != "" {
		t.Errorf("expected empty docker version, got %q", info.DockerVersion)
	}
	if !info.DockerUnreachable {
		t.Error("expected docker to be reported as unreachable")
	}
}

func TestSendAgentInfo_DockerWentAwayKeepsLastVersion(t *testing.T) {
	sender := &stubSender{sent: make(chan *messages.ClientMessage, 1)}

	sendAgentInfo(sender, stubDockerState{version: "28.1.1"})

	info := receiveAgentInfo(t, sender)
	if info.DockerVersion != "28.1.1" {
		t.Errorf("expected docker version %q, got %q", "28.1.1", info.DockerVersion)
	}
	if !info.DockerUnreachable {
		t.Error("expected docker to be reported as unreachable")
	}
}

func TestSendAgentInfo_WithoutConnectionIsNoop(t *testing.T) {
	sendAgentInfo(&senderRef{}, stubDockerState{ready: true, version: "28.1.1"})
}
