package docker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestResolveHostDeploymentsDirUsesDetectedPath(t *testing.T) {
	calls := 0
	c := &Client{
		log:            zerolog.Nop(),
		deploymentsDir: "/deployments",
		detectHostDeploymentsDir: func(context.Context) (string, error) {
			calls++
			return "/srv/orcacd/deployments", nil
		},
	}

	c.resolveHostDeploymentsDir(t.Context())

	if got := c.hostDeploymentsBase(); got != "/srv/orcacd/deployments" {
		t.Errorf("host deployments dir = %q, want %q", got, "/srv/orcacd/deployments")
	}
	if calls != 1 {
		t.Errorf("detector calls = %d, want 1", calls)
	}
}

func TestResolveHostDeploymentsDirRetriesAfterFailure(t *testing.T) {
	calls := 0
	c := &Client{
		log:            zerolog.Nop(),
		deploymentsDir: "/deployments",
		detectHostDeploymentsDir: func(context.Context) (string, error) {
			calls++
			if calls == 1 {
				return "", errors.New("temporary inspect failure")
			}
			return "/srv/orcacd/deployments", nil
		},
	}

	c.resolveHostDeploymentsDir(t.Context())
	c.resolveHostDeploymentsDir(t.Context())

	if got := c.hostDeploymentsBase(); got != "/srv/orcacd/deployments" {
		t.Errorf("host deployments dir = %q, want detected path", got)
	}
	if calls != 2 {
		t.Errorf("detector calls = %d, want 2", calls)
	}
}

func TestResolveHostDeploymentsDirCoalescesConcurrentCalls(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	c := &Client{
		log:            zerolog.Nop(),
		deploymentsDir: "/deployments",
		detectHostDeploymentsDir: func(context.Context) (string, error) {
			if calls.Add(1) == 1 {
				close(started)
			}
			<-release
			return "/srv/orcacd/deployments", nil
		},
	}

	firstDone := make(chan struct{})
	go func() {
		c.resolveHostDeploymentsDir(t.Context())
		close(firstDone)
	}()
	<-started

	secondDone := make(chan struct{})
	go func() {
		c.resolveHostDeploymentsDir(t.Context())
		close(secondDone)
	}()

	select {
	case <-secondDone:
		t.Fatal("concurrent resolver returned before in-flight detection completed")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	for _, done := range []<-chan struct{}{firstDone, secondDone} {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("resolver did not finish")
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("detector calls = %d, want 1", got)
	}
}

func newTestClient(t *testing.T) *Client {
	t.Helper()
	return newTestClientWithAllowlist(t, nil)
}

func newTestClientWithAllowlist(t *testing.T, allowedPrivilegedApps map[string]struct{}) *Client {
	t.Helper()
	c, err := New(zerolog.Nop(), t.TempDir(), allowedPrivilegedApps, false)
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

	c, err := New(zerolog.Nop(), t.TempDir(), nil, false)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.Ready() {
		t.Error("expected Ready() == false with unreachable Docker host")
	}
}

func TestPingDaemon_Unreachable(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://localhost:1")

	c, err := New(zerolog.Nop(), t.TempDir(), nil, false)
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
