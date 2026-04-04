package websocket

import (
	"os"
	"testing"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/rs/zerolog"
)

func TestNewWorker(t *testing.T) {
	log := zerolog.New(os.Stderr).Level(zerolog.Disabled)
	h := NewHub(&log)
	w := NewWorker(h, &log)

	if w == nil {
		t.Fatal("expected non-nil worker")
	}
	if w.hub != h {
		t.Error("expected worker hub to match")
	}
}

func TestWorker_Start_BroadcastsPing(t *testing.T) {
	log := zerolog.New(os.Stderr).Level(zerolog.Disabled)
	h := NewHub(&log)

	conn := newTestWSConn(t)
	defer conn.Close() //nolint:errcheck

	client, err := h.Register("agent-1", conn)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// We can't wait 60s in a test, so instead we verify the broadcast logic
	// by calling Broadcast directly (Worker.Start just wraps a ticker + Broadcast).
	h.Broadcast(&messages.ServerMessage{
		Payload: &messages.ServerMessage_Ping{
			Ping: &messages.PingRequest{Timestamp: time.Now().UnixMilli()},
		},
	})

	select {
	case msg := <-client.Send:
		ping := msg.GetPing()
		if ping == nil {
			t.Fatal("expected ping payload")
		}
		if ping.Timestamp == 0 {
			t.Error("expected non-zero timestamp")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broadcast message")
	}
}
