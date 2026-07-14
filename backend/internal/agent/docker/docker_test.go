package docker

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

type observedDoneContext struct {
	context.Context
	doneObserved chan struct{}
	once         sync.Once
}

func (c *observedDoneContext) Done() <-chan struct{} {
	c.once.Do(func() {
		close(c.doneObserved)
	})
	return c.Context.Done()
}

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

func TestResolveHostDeploymentsDirCachesHostExecution(t *testing.T) {
	calls := 0
	c := &Client{
		log: zerolog.Nop(),
		detectHostDeploymentsDir: func(context.Context) (string, error) {
			calls++
			return "", errNotContainerized
		},
	}

	c.resolveHostDeploymentsDir(t.Context())
	c.resolveHostDeploymentsDir(t.Context())

	if calls != 1 {
		t.Errorf("detector calls = %d, want 1", calls)
	}
	if got := c.hostDeploymentsBase(); got != "" {
		t.Errorf("host deployments dir = %q, want empty", got)
	}
}

func TestRunHostDeploymentsDirDetectionRequiresDetector(t *testing.T) {
	if _, err := runHostDeploymentsDirDetection(t.Context(), nil); err == nil {
		t.Fatal("expected missing detector error")
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

func TestResolveHostDeploymentsDirWaiterRetriesAfterTransientFailure(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	c := &Client{
		log: zerolog.Nop(),
		detectHostDeploymentsDir: func(context.Context) (string, error) {
			if calls.Add(1) == 1 {
				close(started)
				<-release
				return "", errors.New("temporary inspect failure")
			}
			return "/srv/orcacd/deployments", nil
		},
	}

	ownerDone := make(chan struct{})
	go func() {
		c.resolveHostDeploymentsDir(t.Context())
		close(ownerDone)
	}()
	<-started

	waiterCtx := &observedDoneContext{Context: t.Context(), doneObserved: make(chan struct{})}
	waiterDone := make(chan struct{})
	go func() {
		c.resolveHostDeploymentsDir(waiterCtx)
		close(waiterDone)
	}()
	<-waiterCtx.doneObserved

	close(release)
	for _, done := range []<-chan struct{}{ownerDone, waiterDone} {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("resolver did not finish")
		}
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("detector calls = %d, want 2", got)
	}
	if got := c.hostDeploymentsBase(); got != "/srv/orcacd/deployments" {
		t.Errorf("host deployments dir = %q, want detected path", got)
	}
}

func TestResolveHostDeploymentsDirWaiterRetriesAfterOwnerCancellation(t *testing.T) {
	started := make(chan struct{})
	var calls atomic.Int32
	c := &Client{
		log: zerolog.Nop(),
		detectHostDeploymentsDir: func(ctx context.Context) (string, error) {
			if calls.Add(1) == 1 {
				close(started)
				<-ctx.Done()
				return "", ctx.Err()
			}
			return "/srv/orcacd/deployments", nil
		},
	}

	ownerCtx, cancelOwner := context.WithCancel(t.Context())
	ownerDone := make(chan struct{})
	go func() {
		c.resolveHostDeploymentsDir(ownerCtx)
		close(ownerDone)
	}()
	<-started

	waiterCtx := &observedDoneContext{Context: t.Context(), doneObserved: make(chan struct{})}
	waiterDone := make(chan struct{})
	go func() {
		c.resolveHostDeploymentsDir(waiterCtx)
		close(waiterDone)
	}()
	<-waiterCtx.doneObserved
	cancelOwner()

	for _, done := range []<-chan struct{}{ownerDone, waiterDone} {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("resolver did not finish")
		}
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("detector calls = %d, want 2", got)
	}
	if got := c.hostDeploymentsBase(); got != "/srv/orcacd/deployments" {
		t.Errorf("host deployments dir = %q, want detected path", got)
	}
}

func TestResolveHostDeploymentsDirCanceledWaiterReturnsWithoutCancelingOwner(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	c := &Client{
		log: zerolog.Nop(),
		detectHostDeploymentsDir: func(context.Context) (string, error) {
			calls.Add(1)
			close(started)
			<-release
			return "/srv/orcacd/deployments", nil
		},
	}

	ownerDone := make(chan struct{})
	go func() {
		c.resolveHostDeploymentsDir(t.Context())
		close(ownerDone)
	}()
	<-started

	baseWaiterCtx, cancelWaiter := context.WithCancel(t.Context())
	waiterCtx := &observedDoneContext{Context: baseWaiterCtx, doneObserved: make(chan struct{})}
	waiterDone := make(chan struct{})
	go func() {
		c.resolveHostDeploymentsDir(waiterCtx)
		close(waiterDone)
	}()
	<-waiterCtx.doneObserved
	cancelWaiter()

	select {
	case <-waiterDone:
	case <-time.After(time.Second):
		t.Fatal("canceled waiter did not return")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("detector calls = %d, want 1", got)
	}

	close(release)
	select {
	case <-ownerDone:
	case <-time.After(time.Second):
		t.Fatal("owner did not finish")
	}
	if got := c.hostDeploymentsBase(); got != "/srv/orcacd/deployments" {
		t.Errorf("host deployments dir = %q, want detected path", got)
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
