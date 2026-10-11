package websocket

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/rs/zerolog"
)

type presenceEvent struct {
	agentId string
	event   models.NotificationEvent
}

// fakePresenceClock replaces time.AfterFunc so tests fire grace periods explicitly.
type fakePresenceClock struct {
	mu     sync.Mutex
	timers []*fakePresenceTimer
}

type fakePresenceTimer struct {
	delay   time.Duration
	fn      func()
	stopped bool
}

func (c *fakePresenceClock) afterFunc(d time.Duration, fn func()) func() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	timer := &fakePresenceTimer{delay: d, fn: fn}
	c.timers = append(c.timers, timer)
	return func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		wasActive := !timer.stopped
		timer.stopped = true
		return wasActive
	}
}

// fireAll runs every timer that was not stopped, like the grace period elapsing.
func (c *fakePresenceClock) fireAll() {
	c.mu.Lock()
	var due []func()
	for _, timer := range c.timers {
		if !timer.stopped {
			timer.stopped = true
			due = append(due, timer.fn)
		}
	}
	c.mu.Unlock()
	for _, fn := range due {
		fn()
	}
}

type presenceRecorder struct {
	mu     sync.Mutex
	events []presenceEvent
}

func (r *presenceRecorder) notify(agentId string, event models.NotificationEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, presenceEvent{agentId: agentId, event: event})
}

func (r *presenceRecorder) get() []presenceEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.events)
}

func newTestPresenceNotifier() (*agentPresenceNotifier, *fakePresenceClock, *presenceRecorder) {
	recorder := &presenceRecorder{}
	clock := &fakePresenceClock{}
	notifier := newAgentPresenceNotifier(agentOfflineGracePeriod, recorder.notify)
	notifier.afterFunc = clock.afterFunc
	return notifier, clock, recorder
}

func assertPresenceEvents(t *testing.T, recorder *presenceRecorder, want ...presenceEvent) {
	t.Helper()
	if got := recorder.get(); !slices.Equal(got, want) {
		t.Fatalf("expected presence events %v, got %v", want, got)
	}
}

func TestAgentPresence_ReportsOfflineAfterGracePeriod(t *testing.T) {
	notifier, clock, recorder := newTestPresenceNotifier()

	notifier.Disconnected("agent-1")
	assertPresenceEvents(t, recorder)
	if len(clock.timers) != 1 || clock.timers[0].delay != agentOfflineGracePeriod {
		t.Fatalf("expected one timer with the grace period, got %+v", clock.timers)
	}

	clock.fireAll()
	assertPresenceEvents(t, recorder, presenceEvent{"agent-1", models.NotificationEventAgentOffline})
}

func TestAgentPresence_ReconnectWithinGracePeriodIsSilent(t *testing.T) {
	notifier, clock, recorder := newTestPresenceNotifier()

	notifier.Disconnected("agent-1")
	notifier.Connected("agent-1")
	clock.fireAll()

	assertPresenceEvents(t, recorder)
}

func TestAgentPresence_StaleTimerDoesNotReportAfterReconnect(t *testing.T) {
	notifier, clock, recorder := newTestPresenceNotifier()

	notifier.Disconnected("agent-1")
	// Simulates a timer that already fired and waits for the lock while the
	// agent reconnects: the stale callback must not report the agent offline.
	staleCallback := clock.timers[0].fn
	notifier.Connected("agent-1")
	staleCallback()

	assertPresenceEvents(t, recorder)
}

func TestAgentPresence_OnlineOnlyAfterOfflineWasReported(t *testing.T) {
	notifier, clock, recorder := newTestPresenceNotifier()

	// A first connect, e.g. after a hub restart, must not announce the agent.
	notifier.Connected("agent-1")
	assertPresenceEvents(t, recorder)

	notifier.Disconnected("agent-1")
	clock.fireAll()
	notifier.Connected("agent-1")
	assertPresenceEvents(t, recorder,
		presenceEvent{"agent-1", models.NotificationEventAgentOffline},
		presenceEvent{"agent-1", models.NotificationEventAgentOnline},
	)

	// The online report resets the state, so the next connect is silent again.
	notifier.Connected("agent-1")
	assertPresenceEvents(t, recorder,
		presenceEvent{"agent-1", models.NotificationEventAgentOffline},
		presenceEvent{"agent-1", models.NotificationEventAgentOnline},
	)
}

func TestAgentPresence_TracksAgentsIndependently(t *testing.T) {
	notifier, clock, recorder := newTestPresenceNotifier()

	notifier.Disconnected("agent-1")
	notifier.Disconnected("agent-2")
	notifier.Connected("agent-2")
	clock.fireAll()

	assertPresenceEvents(t, recorder, presenceEvent{"agent-1", models.NotificationEventAgentOffline})
}

func TestAgentPresence_StopDiscardsPendingAndIgnoresLaterEvents(t *testing.T) {
	notifier, clock, recorder := newTestPresenceNotifier()

	notifier.Disconnected("agent-1")
	staleCallback := clock.timers[0].fn
	notifier.Stop()
	staleCallback()
	clock.fireAll()

	// Connections closing during shutdown must not start new grace periods.
	notifier.Disconnected("agent-2")
	clock.fireAll()

	assertPresenceEvents(t, recorder)
	if len(notifier.pending) != 0 {
		t.Fatalf("expected no pending timers after Stop, got %d", len(notifier.pending))
	}
}

func TestAgentPresence_StopSuppressesOnlineAfterOffline(t *testing.T) {
	notifier, clock, recorder := newTestPresenceNotifier()

	notifier.Disconnected("agent-1")
	clock.fireAll()
	notifier.Stop()
	notifier.Connected("agent-1")

	assertPresenceEvents(t, recorder, presenceEvent{"agent-1", models.NotificationEventAgentOffline})
}

func TestAgentPresence_RealTimerFires(t *testing.T) {
	events := make(chan presenceEvent, 1)
	notifier := newAgentPresenceNotifier(time.Millisecond, func(agentId string, event models.NotificationEvent) {
		events <- presenceEvent{agentId, event}
	})

	notifier.Disconnected("agent-1")

	select {
	case got := <-events:
		if got != (presenceEvent{"agent-1", models.NotificationEventAgentOffline}) {
			t.Fatalf("unexpected presence event %v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("offline notification was not sent")
	}
}

func TestAgentPresence_ConcurrentUse(t *testing.T) {
	notifier := newAgentPresenceNotifier(time.Microsecond, func(string, models.NotificationEvent) {})

	var wg sync.WaitGroup
	for i := range 8 {
		agentId := string(rune('a' + i))
		wg.Go(func() {
			for range 100 {
				notifier.Disconnected(agentId)
				notifier.Connected(agentId)
			}
		})
	}
	wg.Wait()
	notifier.Stop()
}

func TestHubShutdownStopsPresenceNotifier(t *testing.T) {
	log := zerolog.Nop()
	hub := NewHub(&log)
	hub.Shutdown()

	hub.presence.Disconnected("agent-1")
	if len(hub.presence.pending) != 0 {
		t.Fatal("expected disconnects after shutdown to be ignored")
	}
}
