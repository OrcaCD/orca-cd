package docker

import (
	"testing"

	"github.com/rs/zerolog"
)

func newTestClient(t *testing.T) *Client {
	t.Helper()
	c, err := New(zerolog.Nop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

func TestNew_Ready(t *testing.T) {
	c := newTestClient(t)
	if !c.Ready() {
		t.Error("expected Ready() == true with Docker available")
	}
}

func TestNew_Accessors(t *testing.T) {
	c := newTestClient(t)
	if c.CLI() == nil {
		t.Error("CLI() is nil")
	}
	if c.Compose() == nil {
		t.Error("Compose() is nil")
	}
}

func TestPingDaemon(t *testing.T) {
	c := newTestClient(t)
	if !c.pingDaemon() {
		t.Error("pingDaemon() returned false with Docker available")
	}
	if !c.Ready() {
		t.Error("expected Ready() == true after successful ping")
	}
	// Idempotent
	if !c.pingDaemon() {
		t.Error("second pingDaemon() returned false")
	}
}

func TestNew_DaemonUnreachable(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://localhost:1")

	c, err := New(zerolog.Nop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.Ready() {
		t.Error("expected Ready() == false with unreachable Docker host")
	}
}

func TestPingDaemon_Unreachable(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://localhost:1")

	c, err := New(zerolog.Nop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.pingDaemon() {
		t.Error("expected pingDaemon() == false with unreachable Docker host")
	}
	if c.Ready() {
		t.Error("expected Ready() == false after failed ping")
	}
}
