package docker

import (
	"context"
	"testing"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/moby/moby/api/types/events"
	"github.com/rs/zerolog"
)

func newTestWatcher(t *testing.T, sender MessageSender, check func(ctx context.Context, appID string) HealthState) *healthWatcher {
	t.Helper()
	w := &healthWatcher{
		ctx:          t.Context(),
		log:          zerolog.Nop(),
		debounce:     time.Millisecond,
		maxDelay:     10 * time.Millisecond,
		checkHealth:  check,
		sender:       sender,
		pending:      make(map[string]*pendingHealthEvaluation),
		lastReported: make(map[string]HealthState),
	}
	t.Cleanup(w.stop)
	return w
}

func staticHealth(state HealthState) func(context.Context, string) HealthState {
	return func(context.Context, string) HealthState { return state }
}

// awaitReports polls the sender until it has received at least n messages.
func awaitReports(t *testing.T, sender *stubMessageSender, n int) []*messages.ClientMessage {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		msgs := sender.received()
		if len(msgs) >= n {
			return msgs
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d messages, got %d", n, len(msgs))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestHealthWatcher_ReportsSettledHealth(t *testing.T) {
	sender := &stubMessageSender{}
	w := newTestWatcher(t, sender, staticHealth(HealthHealthy))

	w.schedule("app-1")

	msgs := awaitReports(t, sender, 1)
	report := msgs[0].GetApplicationStatusReport()
	if report == nil {
		t.Fatalf("expected ApplicationStatusReport, got %T", msgs[0].Payload)
	}
	if len(report.Statuses) != 1 || report.Statuses[0].ApplicationId != "app-1" {
		t.Fatalf("unexpected statuses: %v", report.Statuses)
	}
	if report.Statuses[0].Health != messages.HealthStatus_HEALTH_STATUS_HEALTHY {
		t.Errorf("expected healthy, got %v", report.Statuses[0].Health)
	}
}

func TestHealthWatcher_DeduplicatesUnchangedHealth(t *testing.T) {
	sender := &stubMessageSender{}
	w := newTestWatcher(t, sender, staticHealth(HealthHealthy))

	w.schedule("app-1")
	awaitReports(t, sender, 1)

	w.schedule("app-1")
	time.Sleep(50 * time.Millisecond)

	if msgs := sender.received(); len(msgs) != 1 {
		t.Errorf("expected 1 message for an unchanged state, got %d", len(msgs))
	}
}

func TestHealthWatcher_ReportsChanges(t *testing.T) {
	sender := &stubMessageSender{}
	state := HealthHealthy
	w := newTestWatcher(t, sender, func(context.Context, string) HealthState { return state })

	w.schedule("app-1")
	awaitReports(t, sender, 1)

	state = HealthUnhealthy
	w.schedule("app-1")

	msgs := awaitReports(t, sender, 2)
	report := msgs[1].GetApplicationStatusReport()
	if report.Statuses[0].Health != messages.HealthStatus_HEALTH_STATUS_UNHEALTHY {
		t.Errorf("expected unhealthy, got %v", report.Statuses[0].Health)
	}
}

func TestHealthWatcher_DoesNotReportUnknown(t *testing.T) {
	sender := &stubMessageSender{}
	w := newTestWatcher(t, sender, staticHealth(HealthUnknown))

	w.schedule("app-1")
	time.Sleep(50 * time.Millisecond)

	if msgs := sender.received(); len(msgs) != 0 {
		t.Errorf("expected no messages for unsettled health, got %d", len(msgs))
	}
}

func TestHealthWatcher_InvalidateForcesReport(t *testing.T) {
	sender := &stubMessageSender{}
	w := newTestWatcher(t, sender, staticHealth(HealthHealthy))

	w.schedule("app-1")
	awaitReports(t, sender, 1)

	w.invalidate("app-1")
	w.schedule("app-1")

	awaitReports(t, sender, 2)
}

func TestHealthWatcher_ForgetCancelsPendingEvaluation(t *testing.T) {
	sender := &stubMessageSender{}
	w := newTestWatcher(t, sender, staticHealth(HealthHealthy))
	w.debounce = 50 * time.Millisecond

	w.schedule("app-1")
	w.forget("app-1")
	time.Sleep(150 * time.Millisecond)

	if msgs := sender.received(); len(msgs) != 0 {
		t.Errorf("expected no messages after forget, got %d", len(msgs))
	}
}

func TestHealthWatcher_ContinuousEventsCannotPostponeEvaluationIndefinitely(t *testing.T) {
	sender := &stubMessageSender{}
	w := newTestWatcher(t, sender, staticHealth(HealthUnhealthy))
	w.debounce = 40 * time.Millisecond
	w.maxDelay = 100 * time.Millisecond

	started := time.Now()
	stopEvents := make(chan struct{})
	eventsDone := make(chan struct{})
	go func() {
		defer close(eventsDone)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopEvents:
				return
			case <-ticker.C:
				w.schedule("app-1")
			}
		}
	}()
	t.Cleanup(func() {
		close(stopEvents)
		<-eventsDone
	})

	w.schedule("app-1")
	awaitReports(t, sender, 1)
	if elapsed := time.Since(started); elapsed >= 250*time.Millisecond {
		t.Errorf("continuous events postponed evaluation for %s", elapsed)
	}
}

func TestHealthWatcher_NilSenderStillTracksState(t *testing.T) {
	w := newTestWatcher(t, nil, staticHealth(HealthHealthy))

	w.schedule("app-1")

	deadline := time.Now().Add(2 * time.Second)
	for {
		w.mu.Lock()
		state, seen := w.lastReported["app-1"]
		w.mu.Unlock()
		if seen {
			if state != HealthHealthy {
				t.Errorf("expected HealthHealthy tracked, got %v", state)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for evaluation without a sender")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestIsHealthRelevantAction(t *testing.T) {
	relevant := []events.Action{
		events.ActionStart, events.ActionRestart, events.ActionStop, events.ActionDie,
		events.ActionOOM, events.ActionPause, events.ActionUnPause, events.ActionDestroy,
		events.ActionHealthStatus, events.ActionHealthStatusRunning,
		events.ActionHealthStatusHealthy, events.ActionHealthStatusUnhealthy,
		events.Action("health_status: custom output"),
	}
	for _, action := range relevant {
		if !isHealthRelevantAction(action) {
			t.Errorf("expected %q to be health relevant", action)
		}
	}

	// exec_* events fire for every healthcheck run and must not trigger
	// evaluations.
	irrelevant := []events.Action{
		events.ActionExecCreate, events.ActionExecStart, events.ActionExecDie,
		events.ActionCreate, events.ActionAttach, events.ActionRename,
		events.Action("health_status_changed"),
	}
	for _, action := range irrelevant {
		if isHealthRelevantAction(action) {
			t.Errorf("expected %q to not be health relevant", action)
		}
	}
}

func TestHandleEvent_SchedulesOnlyLabeledContainerEvents(t *testing.T) {
	sender := &stubMessageSender{}
	w := newTestWatcher(t, sender, staticHealth(HealthHealthy))
	c := &Client{healthWatcher: w}

	// Ignored: wrong type, missing label, irrelevant action.
	c.handleEvent(events.Message{Type: events.ImageEventType, Action: events.ActionPull})
	c.handleEvent(events.Message{Type: events.ContainerEventType, Action: events.ActionStart})
	c.handleEvent(events.Message{
		Type:   events.ContainerEventType,
		Action: events.ActionExecDie,
		Actor:  events.Actor{Attributes: map[string]string{labelApplicationID: "app-1"}},
	})
	time.Sleep(50 * time.Millisecond)
	if msgs := sender.received(); len(msgs) != 0 {
		t.Fatalf("expected no messages for ignored events, got %d", len(msgs))
	}

	c.handleEvent(events.Message{
		Type:   events.ContainerEventType,
		Action: events.ActionHealthStatusHealthy,
		Actor:  events.Actor{Attributes: map[string]string{labelApplicationID: "app-1"}},
	})

	msgs := awaitReports(t, sender, 1)
	report := msgs[0].GetApplicationStatusReport()
	if report == nil || report.Statuses[0].ApplicationId != "app-1" {
		t.Fatalf("expected health report for app-1, got %v", msgs[0].Payload)
	}
}

func TestReconcileApplicationHealth_ReEvaluatesActiveApps(t *testing.T) {
	c := newTestClient(t)
	sender := &stubMessageSender{}
	c.healthWatcher.sender = sender
	c.healthWatcher.debounce = time.Millisecond
	c.healthWatcher.checkHealth = staticHealth(HealthHealthy)
	c.healthWatcher.lastReported["app-1"] = HealthUnhealthy

	c.reconcileApplicationHealth(map[string]struct{}{"app-1": {}})

	deadline := time.Now().Add(2 * time.Second)
	for {
		var found bool
		for _, msg := range sender.received() {
			if report := msg.GetApplicationStatusReport(); report != nil {
				for _, status := range report.Statuses {
					if status.ApplicationId == "app-1" && status.Health == messages.HealthStatus_HEALTH_STATUS_HEALTHY {
						found = true
					}
				}
			}
		}
		if found {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for resync to re-report app-1")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestReconcileApplicationHealth_ForgetsAppsWithoutContainers(t *testing.T) {
	c := newTestClient(t)
	c.healthWatcher.debounce = time.Hour
	c.healthWatcher.lastReported["removed-app"] = HealthHealthy
	c.healthWatcher.schedule("removed-app")

	c.reconcileApplicationHealth(map[string]struct{}{})

	if _, known := c.healthWatcher.knownApps()["removed-app"]; known {
		t.Error("expected app without containers to be removed from known apps")
	}
	c.healthWatcher.mu.Lock()
	_, pending := c.healthWatcher.pending["removed-app"]
	c.healthWatcher.mu.Unlock()
	if pending {
		t.Error("expected pending evaluation for removed app to be cancelled")
	}
}
