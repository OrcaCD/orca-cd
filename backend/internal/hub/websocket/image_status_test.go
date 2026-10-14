package websocket

import (
	"context"
	"errors"
	"testing"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
)

func TestRequestImageStatus_RoundTrip(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)

	client, err := h.Register("agent-1", nil)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	type res struct {
		result *messages.ImageStatusResult
		err    error
	}
	done := make(chan res, 1)
	go func() {
		result, err := h.RequestImageStatus(context.Background(), "agent-1", "app-1", "billing", true)
		done <- res{result, err}
	}()

	var req *messages.ImageStatusRequest
	select {
	case msg := <-client.Send:
		req = msg.GetImageStatusRequest()
		if req == nil {
			t.Fatalf("expected ImageStatusRequest, got %T", msg.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("hub did not send an ImageStatusRequest")
	}
	if req.ApplicationId != "app-1" || req.ApplicationName != "billing" || !req.CheckLatest || req.RequestId == "" {
		t.Fatalf("unexpected request: %+v", req)
	}

	// Another agent must not be able to answer the request.
	if h.ResolveImageStatusResult("agent-2", &messages.ImageStatusResult{RequestId: req.RequestId}) {
		t.Fatal("expected result from a different agent to be rejected")
	}

	images := []*messages.ServiceImageStatus{{Service: "web", Image: "nginx", CurrentDigest: "sha256:abc"}}
	if !h.ResolveImageStatusResult("agent-1", &messages.ImageStatusResult{RequestId: req.RequestId, Images: images}) {
		t.Fatal("expected result to be delivered")
	}

	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("RequestImageStatus: %v", r.err)
		}
		if len(r.result.Images) != 1 || r.result.Images[0].CurrentDigest != "sha256:abc" {
			t.Fatalf("unexpected result: %+v", r.result)
		}
	case <-time.After(time.Second):
		t.Fatal("RequestImageStatus did not return")
	}

	if h.ResolveImageStatusResult("agent-1", &messages.ImageStatusResult{RequestId: req.RequestId}) {
		t.Fatal("expected duplicate result to be ignored")
	}
}

func TestRequestImageStatus_AgentOffline(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)

	_, err := h.RequestImageStatus(context.Background(), "missing", "app-1", "billing", false)
	if !errors.Is(err, ErrAgentOffline) {
		t.Fatalf("expected ErrAgentOffline, got %v", err)
	}
	if len(h.pendingImageStatus) != 0 {
		t.Fatal("expected pending request to be cleaned up")
	}
}

func TestRequestImageStatus_ContextCancelled(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)
	if _, err := h.Register("agent-1", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := h.RequestImageStatus(ctx, "agent-1", "app-1", "billing", false)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}
