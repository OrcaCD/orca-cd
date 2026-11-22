package websocket

import (
	"context"
	"errors"
	"testing"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
)

func TestListContainers_RoundTrip(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)
	client := registerEnvTestClient(t, h)

	type res struct {
		result *messages.ContainerListResult
		err    error
	}
	done := make(chan res, 1)
	go func() {
		result, err := h.ListContainers(context.Background(), "agent-1", "app-1")
		done <- res{result, err}
	}()

	req := receiveServerMessage(t, client).GetContainerListRequest()
	if req == nil {
		t.Fatal("expected ContainerListRequest")
	}
	if req.ApplicationId != "app-1" {
		t.Fatalf("unexpected request: %+v", req)
	}

	if !h.ResolveContainerListResult("agent-1", &messages.ContainerListResult{
		RequestId:     req.RequestId,
		ApplicationId: "app-1",
		Success:       true,
		Containers:    []*messages.ContainerInfo{{Id: "abc", Service: "web"}},
	}) {
		t.Fatal("expected ResolveContainerListResult to find the waiting caller")
	}

	select {
	case r := <-done:
		if r.err != nil || r.result == nil || len(r.result.Containers) != 1 || r.result.Containers[0].Service != "web" {
			t.Fatalf("unexpected outcome: %+v, %v", r.result, r.err)
		}
	case <-time.After(time.Second):
		t.Fatal("ListContainers did not return")
	}
}

func TestInspectContainer_RoundTrip(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)
	client := registerEnvTestClient(t, h)

	done := make(chan *messages.ContainerInspectResult, 1)
	go func() {
		result, err := h.InspectContainer(context.Background(), "agent-1", "app-1", "abc123")
		if err != nil {
			t.Errorf("InspectContainer: %v", err)
		}
		done <- result
	}()

	req := receiveServerMessage(t, client).GetContainerInspectRequest()
	if req == nil {
		t.Fatal("expected ContainerInspectRequest")
	}
	if req.ApplicationId != "app-1" || req.ContainerId != "abc123" {
		t.Fatalf("unexpected request: %+v", req)
	}

	h.ResolveContainerInspectResult("agent-1", &messages.ContainerInspectResult{
		RequestId:     req.RequestId,
		ApplicationId: "app-1",
		Success:       true,
		InspectJson:   "{}",
	})

	select {
	case result := <-done:
		if result == nil || result.InspectJson != "{}" {
			t.Fatalf("unexpected result: %+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("InspectContainer did not return")
	}
}

func TestContainerRequests_AgentOffline(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)

	if _, err := h.ListContainers(context.Background(), "missing", "app-1"); !errors.Is(err, ErrAgentOffline) {
		t.Fatalf("ListContainers error = %v, want ErrAgentOffline", err)
	}
	if _, err := h.InspectContainer(context.Background(), "missing", "app-1", "abc"); !errors.Is(err, ErrAgentOffline) {
		t.Fatalf("InspectContainer error = %v, want ErrAgentOffline", err)
	}
}

func TestContainerRequests_ContextCancelled(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)
	registerEnvTestClient(t, h)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if _, err := h.ListContainers(ctx, "agent-1", "app-1"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want DeadlineExceeded", err)
	}

	h.requestMu.Lock()
	defer h.requestMu.Unlock()
	if len(h.pendingContainerLists) != 0 {
		t.Fatalf("pending request leaked: %d", len(h.pendingContainerLists))
	}
}

func TestResolveContainerResult_RejectsMismatches(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)
	client := registerEnvTestClient(t, h)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = h.InspectContainer(context.Background(), "agent-1", "app-1", "abc")
	}()
	req := receiveServerMessage(t, client).GetContainerInspectRequest()

	if h.ResolveContainerInspectResult("agent-2", &messages.ContainerInspectResult{RequestId: req.RequestId, ApplicationId: "app-1"}) {
		t.Error("resolved by a different agent")
	}
	if h.ResolveContainerInspectResult("agent-1", &messages.ContainerInspectResult{RequestId: req.RequestId, ApplicationId: "app-2"}) {
		t.Error("resolved with a different application")
	}
	if h.ResolveContainerListResult("agent-1", &messages.ContainerListResult{RequestId: req.RequestId, ApplicationId: "app-1"}) {
		t.Error("list result resolved an inspect request")
	}

	if !h.ResolveContainerInspectResult("agent-1", &messages.ContainerInspectResult{RequestId: req.RequestId, ApplicationId: "app-1"}) {
		t.Fatal("expected the correct result to resolve the request")
	}
	<-done
}

func TestContainerRequests_AbortedOnDisconnect(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)
	client := registerEnvTestClient(t, h)

	errCh := make(chan error, 1)
	go func() {
		_, err := h.ListContainers(context.Background(), "agent-1", "app-1")
		errCh <- err
	}()
	receiveServerMessage(t, client)

	h.BeginDisconnect(client)
	h.FinishDisconnect(client)

	select {
	case err := <-errCh:
		if !errors.Is(err, ErrAgentOffline) {
			t.Fatalf("error = %v, want ErrAgentOffline", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ListContainers was not woken by the disconnect")
	}
}
