package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	agentdocker "github.com/OrcaCD/orca-cd/internal/agent/docker"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/shared/wscrypto"
)

var _ containerManager = (*agentdocker.Client)(nil)

type stubContainerDeployer struct {
	stubDeployer

	mu         sync.Mutex
	listCalls  []string
	inspectIDs []string
	containers []*messages.ContainerInfo
	inspect    string
	err        error
	deadline   time.Time
}

func (d *stubContainerDeployer) ListContainers(ctx context.Context, appID string) ([]*messages.ContainerInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.listCalls = append(d.listCalls, appID)
	d.deadline, _ = ctx.Deadline()
	return d.containers, d.err
}

func (d *stubContainerDeployer) InspectContainer(_ context.Context, appID, containerID string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.inspectIDs = append(d.inspectIDs, appID+"/"+containerID)
	return d.inspect, d.err
}

func TestExecuteContainerList_Success(t *testing.T) {
	sender := &stubSender{sent: make(chan *messages.ClientMessage, 1)}
	deployer := &stubContainerDeployer{containers: []*messages.ContainerInfo{{Id: "c1", Service: "web"}}}

	executeContainerList(t.Context(), sender, deployer, &messages.ContainerListRequest{RequestId: "req-1", ApplicationId: "app-1"})

	result := receiveMessage(t, sender).GetContainerListResult()
	if result == nil {
		t.Fatal("expected ContainerListResult payload")
	}
	if !result.Success || result.RequestId != "req-1" || result.ApplicationId != "app-1" {
		t.Errorf("unexpected result: %+v", result)
	}
	if len(result.Containers) != 1 || result.Containers[0].Service != "web" {
		t.Errorf("containers = %v", result.Containers)
	}
	if len(deployer.listCalls) != 1 || deployer.listCalls[0] != "app-1" {
		t.Errorf("listCalls = %v", deployer.listCalls)
	}
	if remaining := time.Until(deployer.deadline); remaining <= 0 || remaining > containerRequestTimeout {
		t.Errorf("expected a deadline within %s, got %s", containerRequestTimeout, remaining)
	}
}

func TestExecuteContainerList_Error(t *testing.T) {
	sender := &stubSender{sent: make(chan *messages.ClientMessage, 1)}
	deployer := &stubContainerDeployer{err: agentdocker.ErrDaemonUnreachable}

	executeContainerList(t.Context(), sender, deployer, &messages.ContainerListRequest{RequestId: "req-1", ApplicationId: "app-1"})

	result := receiveMessage(t, sender).GetContainerListResult()
	if result.Success || result.ErrorMessage != agentdocker.ErrDaemonUnreachable.Error() {
		t.Errorf("unexpected result: %+v", result)
	}
	if len(result.Containers) != 0 {
		t.Errorf("a failed result must not carry containers: %v", result.Containers)
	}
}

func TestExecuteContainerList_Unsupported(t *testing.T) {
	sender := &stubSender{sent: make(chan *messages.ClientMessage, 1)}

	executeContainerList(t.Context(), sender, &stubDeployer{}, &messages.ContainerListRequest{RequestId: "req-1", ApplicationId: "app-1"})

	result := receiveMessage(t, sender).GetContainerListResult()
	if result.Success || result.ErrorMessage != errContainersUnsupported {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestExecuteContainerInspect_Success(t *testing.T) {
	sender := &stubSender{sent: make(chan *messages.ClientMessage, 1)}
	deployer := &stubContainerDeployer{inspect: `{"Id":"c1"}`}

	executeContainerInspect(t.Context(), sender, deployer, &messages.ContainerInspectRequest{
		RequestId: "req-1", ApplicationId: "app-1", ContainerId: "c1",
	})

	result := receiveMessage(t, sender).GetContainerInspectResult()
	if result == nil {
		t.Fatal("expected ContainerInspectResult payload")
	}
	if !result.Success || result.InspectJson != `{"Id":"c1"}` || result.RequestId != "req-1" || result.ApplicationId != "app-1" {
		t.Errorf("unexpected result: %+v", result)
	}
	if len(deployer.inspectIDs) != 1 || deployer.inspectIDs[0] != "app-1/c1" {
		t.Errorf("inspectIDs = %v", deployer.inspectIDs)
	}
}

func TestExecuteContainerInspect_NotFound(t *testing.T) {
	sender := &stubSender{sent: make(chan *messages.ClientMessage, 1)}
	deployer := &stubContainerDeployer{err: agentdocker.ErrContainerNotFound}

	executeContainerInspect(t.Context(), sender, deployer, &messages.ContainerInspectRequest{
		RequestId: "req-1", ApplicationId: "app-1", ContainerId: "other",
	})

	result := receiveMessage(t, sender).GetContainerInspectResult()
	if result.Success || result.ErrorMessage != agentdocker.ErrContainerNotFound.Error() || result.InspectJson != "" {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestExecuteContainerInspect_Unsupported(t *testing.T) {
	sender := &stubSender{sent: make(chan *messages.ClientMessage, 1)}

	executeContainerInspect(t.Context(), sender, &stubDeployer{}, &messages.ContainerInspectRequest{RequestId: "req-1"})

	result := receiveMessage(t, sender).GetContainerInspectResult()
	if result.Success || result.ErrorMessage != errContainersUnsupported {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestExecuteContainerInspect_SendFailureDoesNotPanic(t *testing.T) {
	sender := &stubSender{err: errors.New("connection closed")}
	deployer := &stubContainerDeployer{inspect: "{}"}

	executeContainerInspect(t.Context(), sender, deployer, &messages.ContainerInspectRequest{RequestId: "req-1"})
}

func TestHandleServerMessage_ContainerRequests(t *testing.T) {
	session, err := wscrypto.NewSession(make([]byte, 32), wscrypto.RoleAgent)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	hub := newHubTestSession(t)
	sender := &stubSender{sent: make(chan *messages.ClientMessage, 2)}
	deployer := &stubContainerDeployer{inspect: "{}"}

	for _, payload := range []*messages.ServerMessage{
		{Payload: &messages.ServerMessage_ContainerListRequest{ContainerListRequest: &messages.ContainerListRequest{RequestId: "list-1", ApplicationId: "app-h"}}},
		{Payload: &messages.ServerMessage_ContainerInspectRequest{ContainerInspectRequest: &messages.ContainerInspectRequest{RequestId: "inspect-1", ApplicationId: "app-h", ContainerId: "c1"}}},
	} {
		env, err := hub.Encrypt(payload)
		if err != nil {
			t.Fatalf("Encrypt: %v", err)
		}
		handleServerMessage(t.Context(), &messages.ServerMessage{
			Payload: &messages.ServerMessage_EncryptedPayload{EncryptedPayload: env},
		}, session, sender, deployer, nil, newStreamManager())
	}

	got := map[string]bool{}
	for range 2 {
		msg := receiveMessage(t, sender)
		if r := msg.GetContainerListResult(); r != nil {
			got[r.RequestId] = r.Success
		}
		if r := msg.GetContainerInspectResult(); r != nil {
			got[r.RequestId] = r.Success
		}
	}
	if !got["list-1"] || !got["inspect-1"] {
		t.Fatalf("expected both requests to succeed, got %v", got)
	}
}
