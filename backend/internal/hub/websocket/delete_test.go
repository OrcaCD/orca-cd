package websocket

import (
	"context"
	"errors"
	"testing"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
)

func TestRemoveApplication_RoundTrip(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)

	conn := newTestWSConn(t)
	defer conn.Close() //nolint:errcheck
	client, err := h.Register("agent-1", conn)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	type res struct {
		result *messages.DeleteResult
		err    error
	}
	done := make(chan res, 1)
	go func() {
		result, err := h.RemoveApplication(context.Background(), "agent-1", "app-1", "billing")
		done <- res{result, err}
	}()

	// The hub dispatches a DeleteRequest to the agent's Send channel.
	var requestID string
	select {
	case msg := <-client.Send:
		req := msg.GetDeleteRequest()
		if req == nil {
			t.Fatalf("expected DeleteRequest, got %T", msg.Payload)
		}
		if req.ApplicationId != "app-1" || req.ApplicationName != "billing" {
			t.Fatalf("unexpected delete request: %+v", req)
		}
		requestID = req.RequestId
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for delete request")
	}

	if !h.ResolveDeleteResult(&messages.DeleteResult{RequestId: requestID, Success: true}) {
		t.Fatal("expected ResolveDeleteResult to find the waiting caller")
	}

	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("RemoveApplication: %v", r.err)
		}
		if r.result == nil || !r.result.Success {
			t.Fatalf("expected successful result, got %+v", r.result)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for RemoveApplication to return")
	}
}

func TestRemoveApplication_AgentOffline(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)

	_, err := h.RemoveApplication(context.Background(), "ghost", "app-1", "billing")
	if !errors.Is(err, ErrAgentOffline) {
		t.Fatalf("expected ErrAgentOffline, got %v", err)
	}
}

func TestRemoveApplication_ContextCancelled(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)

	conn := newTestWSConn(t)
	defer conn.Close() //nolint:errcheck
	client, err := h.Register("agent-1", conn)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	type res struct {
		result *messages.DeleteResult
		err    error
	}
	done := make(chan res, 1)
	go func() {
		result, err := h.RemoveApplication(ctx, "agent-1", "app-1", "billing")
		done <- res{result, err}
	}()

	// Drain the dispatched request, then cancel without ever resolving.
	select {
	case <-client.Send:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for delete request")
	}
	cancel()

	select {
	case r := <-done:
		if !errors.Is(r.err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", r.err)
		}
		if r.result != nil {
			t.Fatalf("expected nil result on cancellation, got %+v", r.result)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for RemoveApplication to return")
	}
}

func TestResolveDeleteResult_UnknownRequest(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)

	if h.ResolveDeleteResult(&messages.DeleteResult{RequestId: "nope"}) {
		t.Error("expected false for an unknown request id")
	}
}
