package agent

import (
	"context"
	"errors"
	"testing"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/version"
)

type stubDockerVersion struct {
	version string
	err     error
}

func (s stubDockerVersion) ServerVersion(context.Context) (string, error) {
	return s.version, s.err
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

	sendAgentInfo(t.Context(), sender, stubDockerVersion{version: "28.1.1"})

	info := receiveAgentInfo(t, sender)
	if info.Version != version.Version {
		t.Errorf("expected version %q, got %q", version.Version, info.Version)
	}
	if info.DockerVersion != "28.1.1" {
		t.Errorf("expected docker version %q, got %q", "28.1.1", info.DockerVersion)
	}
}

func TestSendAgentInfo_DockerUnavailable(t *testing.T) {
	sender := &stubSender{sent: make(chan *messages.ClientMessage, 1)}

	sendAgentInfo(t.Context(), sender, stubDockerVersion{err: errors.New("daemon unreachable")})

	info := receiveAgentInfo(t, sender)
	if info.Version != version.Version {
		t.Errorf("expected version %q, got %q", version.Version, info.Version)
	}
	if info.DockerVersion != "" {
		t.Errorf("expected empty docker version, got %q", info.DockerVersion)
	}
}
