package websocket

import (
	"context"
	"errors"
	"testing"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
)

func registerEnvTestClient(t *testing.T, h *Hub) *Client {
	t.Helper()
	conn := newTestWSConn(t)
	t.Cleanup(func() { _ = conn.Close() })
	client, err := h.Register("agent-1", conn)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	return client
}

func receiveServerMessage(t *testing.T, client *Client) *messages.ServerMessage {
	t.Helper()
	select {
	case msg := <-client.Send:
		return msg
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for server message")
		return nil
	}
}

func TestListEnv_RoundTrip(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)
	client := registerEnvTestClient(t, h)

	type res struct {
		result *messages.EnvListResult
		err    error
	}
	done := make(chan res, 1)
	go func() {
		result, err := h.ListEnv(context.Background(), "agent-1", "app-1", "billing")
		done <- res{result, err}
	}()

	req := receiveServerMessage(t, client).GetEnvListRequest()
	if req == nil {
		t.Fatal("expected EnvListRequest")
	}
	if req.ApplicationId != "app-1" || req.ApplicationName != "billing" {
		t.Fatalf("unexpected request: %+v", req)
	}

	if !h.ResolveEnvListResult("agent-1", &messages.EnvListResult{RequestId: req.RequestId, ApplicationId: "app-1", Success: true}) {
		t.Fatal("expected ResolveEnvListResult to find the waiting caller")
	}

	select {
	case r := <-done:
		if r.err != nil || r.result == nil || !r.result.Success {
			t.Fatalf("unexpected outcome: %+v, %v", r.result, r.err)
		}
	case <-time.After(time.Second):
		t.Fatal("ListEnv did not return")
	}
}

func TestUpdateEnv_RoundTrip(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)
	client := registerEnvTestClient(t, h)

	files := []*messages.EnvFileChanges{{
		Path:             ".env",
		ExpectedRevision: "rev",
		Changes:          []*messages.EnvChange{{Key: "A", Value: "1"}},
	}}

	done := make(chan *messages.EnvUpdateResult, 1)
	go func() {
		result, err := h.UpdateEnv(context.Background(), "agent-1", "app-1", "billing", files)
		if err != nil {
			t.Errorf("UpdateEnv: %v", err)
		}
		done <- result
	}()

	req := receiveServerMessage(t, client).GetEnvUpdateRequest()
	if req == nil {
		t.Fatal("expected EnvUpdateRequest")
	}
	if len(req.Files) != 1 || req.Files[0].Path != ".env" || req.Files[0].ExpectedRevision != "rev" {
		t.Fatalf("unexpected request: %+v", req)
	}

	h.ResolveEnvUpdateResult("agent-1", &messages.EnvUpdateResult{RequestId: req.RequestId, ApplicationId: "app-1", Conflict: true})

	select {
	case result := <-done:
		if result == nil || !result.Conflict {
			t.Fatalf("unexpected result: %+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("UpdateEnv did not return")
	}
}

func TestEnvRequests_AgentOffline(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)

	if _, err := h.ListEnv(context.Background(), "missing", "app-1", "billing"); !errors.Is(err, ErrAgentOffline) {
		t.Fatalf("ListEnv error = %v, want ErrAgentOffline", err)
	}
	if _, err := h.UpdateEnv(context.Background(), "missing", "app-1", "billing", nil); !errors.Is(err, ErrAgentOffline) {
		t.Fatalf("UpdateEnv error = %v, want ErrAgentOffline", err)
	}
}

func TestEnvRequests_ContextCancelled(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)
	registerEnvTestClient(t, h)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if _, err := h.ListEnv(ctx, "agent-1", "app-1", "billing"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want DeadlineExceeded", err)
	}

	h.envMu.Lock()
	defer h.envMu.Unlock()
	if len(h.pendingEnvLists) != 0 {
		t.Fatalf("pending request leaked: %d", len(h.pendingEnvLists))
	}
}

func TestResolveEnvResult_RejectsMismatches(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)
	client := registerEnvTestClient(t, h)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = h.ListEnv(context.Background(), "agent-1", "app-1", "billing")
	}()
	req := receiveServerMessage(t, client).GetEnvListRequest()

	if h.ResolveEnvListResult("agent-2", &messages.EnvListResult{RequestId: req.RequestId, ApplicationId: "app-1"}) {
		t.Error("resolved by a different agent")
	}
	if h.ResolveEnvListResult("agent-1", &messages.EnvListResult{RequestId: req.RequestId, ApplicationId: "app-2"}) {
		t.Error("resolved with a different application")
	}
	if h.ResolveEnvListResult("agent-1", &messages.EnvListResult{RequestId: "unknown", ApplicationId: "app-1"}) {
		t.Error("resolved an unknown request")
	}

	if h.ResolveEnvUpdateResult("agent-1", &messages.EnvUpdateResult{RequestId: req.RequestId, ApplicationId: "app-1"}) {
		t.Error("update result resolved a list request")
	}

	if !h.ResolveEnvListResult("agent-1", &messages.EnvListResult{RequestId: req.RequestId, ApplicationId: "app-1"}) {
		t.Fatal("expected the correct result to resolve the request")
	}
	<-done
}

func TestEnvRequests_AbortedOnDisconnect(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)
	client := registerEnvTestClient(t, h)

	errCh := make(chan error, 1)
	go func() {
		_, err := h.UpdateEnv(context.Background(), "agent-1", "app-1", "billing", nil)
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
		t.Fatal("UpdateEnv was not woken by the disconnect")
	}
}
