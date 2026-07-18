package docker

import (
	"context"
	"strings"
	"sync"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/moby/moby/api/types/events"
	"github.com/rs/zerolog"
)

const (
	// healthEvaluateDebounce coalesces the burst of container events a compose up
	// produces into a single health evaluation per application.
	healthEvaluateDebounce = time.Second
	// healthEvaluateMaxDelay prevents a continuous event stream, such as a crash
	// loop, from postponing the evaluation indefinitely.
	healthEvaluateMaxDelay = 10 * time.Second
	healthEvaluateTimeout  = 10 * time.Second
)

type pendingHealthEvaluation struct {
	timer    *time.Timer
	deadline time.Time
	epoch    uint64
	ready    bool
}

type healthEvaluation struct {
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	epoch  uint64
}

// healthWatcher turns Docker daemon container events into per-application
// health reports. Events only trigger a (debounced) re-evaluation of the
// application's aggregate health; the hub is notified when the settled state
// differs from what was last reported.
type healthWatcher struct {
	mu           sync.Mutex
	ctx          context.Context
	log          zerolog.Logger
	debounce     time.Duration
	maxDelay     time.Duration
	checkHealth  func(ctx context.Context, appID string) HealthState
	sender       MessageSender
	pending      map[string]*pendingHealthEvaluation
	evaluating   map[string]*healthEvaluation
	epochs       map[string]uint64
	lastReported map[string]HealthState
	nextEpoch    uint64
	stopped      bool
}

func newHealthWatcher(c *Client) *healthWatcher {
	return &healthWatcher{
		ctx:          c.ctx,
		log:          c.log,
		debounce:     healthEvaluateDebounce,
		maxDelay:     healthEvaluateMaxDelay,
		checkHealth:  c.ApplicationHealth,
		pending:      make(map[string]*pendingHealthEvaluation),
		evaluating:   make(map[string]*healthEvaluation),
		epochs:       make(map[string]uint64),
		lastReported: make(map[string]HealthState),
	}
}

// SetHealthReporter wires the sender used to push health changes to the hub.
// Changes observed before a sender is set are tracked but not sent; the full
// status report the agent sends on (re)connect brings the hub up to date.
func (c *Client) SetHealthReporter(sender MessageSender) {
	c.healthWatcher.setSender(sender)
}

// observeApplicationHealth forces the next settled health evaluation for appID
// to be reported even if the state did not change from the agent's point of
// view. Deploys and image updates use this because the hub resets the
// application to unknown and waits for a fresh report.
func (c *Client) observeApplicationHealth(appID string) {
	c.healthWatcher.invalidate(appID)
	c.healthWatcher.schedule(appID)
}

// isHealthRelevantAction reports whether a container event action can change the
// aggregate health derived in aggregateHealth (running state or healthcheck
// status). Notably this excludes the exec_* events emitted for every
// healthcheck run.
func isHealthRelevantAction(action events.Action) bool {
	// ActionHealthStatus is a prefix. Besides the predefined states, Docker may
	// append free-form healthcheck output to it.
	healthStatusPrefix := string(events.ActionHealthStatus)
	if action == events.ActionHealthStatus || strings.HasPrefix(string(action), healthStatusPrefix+": ") {
		return true
	}

	switch action {
	case events.ActionStart, events.ActionRestart, events.ActionStop, events.ActionDie,
		events.ActionOOM, events.ActionPause, events.ActionUnPause, events.ActionDestroy:
		return true
	}
	return false
}

func (w *healthWatcher) setSender(sender MessageSender) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.sender = sender
	w.mu.Unlock()
}

// schedule queues a debounced health evaluation for appID. Repeated events
// extend the debounce window only up to maxDelay from the first event.
func (w *healthWatcher) schedule(appID string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return
	}

	now := time.Now()
	deadline := now.Add(w.maxDelay)
	if w.maxDelay <= 0 {
		deadline = now.Add(w.debounce)
	}
	if pending, ok := w.pending[appID]; ok {
		if pending.ready || !now.Before(pending.deadline) {
			return
		}
		deadline = pending.deadline
		pending.timer.Stop()
	}

	delay := max(time.Duration(0), min(w.debounce, deadline.Sub(now)))
	pending := &pendingHealthEvaluation{
		deadline: deadline,
		epoch:    w.epochForAppLocked(appID),
	}
	pending.timer = time.AfterFunc(delay, func() { w.evaluate(appID, pending) })
	w.pending[appID] = pending
}

func (w *healthWatcher) epochForAppLocked(appID string) uint64 {
	if epoch := w.epochs[appID]; epoch != 0 {
		return epoch
	}
	w.nextEpoch++
	if w.nextEpoch == 0 {
		w.nextEpoch++
	}
	w.epochs[appID] = w.nextEpoch
	return w.nextEpoch
}

// invalidate drops the last reported state so the next settled evaluation is
// always sent.
func (w *healthWatcher) invalidate(appID string) {
	if w == nil {
		return
	}
	w.reset(appID)
}

// forget stops tracking appID entirely, used when the application is removed.
func (w *healthWatcher) forget(appID string) {
	if w == nil {
		return
	}
	w.reset(appID)
}

func (w *healthWatcher) reset(appID string) {
	w.mu.Lock()
	delete(w.lastReported, appID)
	delete(w.epochs, appID)
	if pending, ok := w.pending[appID]; ok {
		pending.timer.Stop()
		delete(w.pending, appID)
	}
	evaluation := w.evaluating[appID]
	if evaluation != nil {
		evaluation.cancel()
	}
	w.mu.Unlock()

	if evaluation != nil {
		<-evaluation.done
	}
}

// stop prevents future evaluations, cancels pending and running work, and
// waits for active evaluations to finish.
func (w *healthWatcher) stop() {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.stopped = true
	for appID, pending := range w.pending {
		pending.timer.Stop()
		delete(w.pending, appID)
	}
	clear(w.epochs)
	evaluations := make([]*healthEvaluation, 0, len(w.evaluating))
	for _, evaluation := range w.evaluating {
		evaluation.cancel()
		evaluations = append(evaluations, evaluation)
	}
	w.mu.Unlock()

	for _, evaluation := range evaluations {
		<-evaluation.done
	}
}

// knownApps returns the ids of all applications a state was reported for.
func (w *healthWatcher) knownApps() map[string]struct{} {
	appIDs := make(map[string]struct{})
	if w == nil {
		return appIDs
	}
	w.mu.Lock()
	for appID := range w.lastReported {
		appIDs[appID] = struct{}{}
	}
	w.mu.Unlock()
	return appIDs
}

// evaluate computes the application's aggregate health and reports it when it
// differs from the last reported state. Unsettled (unknown) states are not
// reported: a healthcheck that is still starting emits another event once it
// settles, and a transiently unreachable daemon is followed by a resync when
// the event stream is re-established.
func (w *healthWatcher) evaluate(appID string, pending *pendingHealthEvaluation) {
	w.mu.Lock()
	if w.stopped || w.pending[appID] != pending || w.epochs[appID] != pending.epoch {
		w.mu.Unlock()
		return
	}
	if w.evaluating[appID] != nil {
		pending.ready = true
		w.mu.Unlock()
		return
	}
	delete(w.pending, appID)
	evaluation := w.newEvaluationLocked(pending.epoch)
	w.evaluating[appID] = evaluation
	w.mu.Unlock()
	w.runEvaluation(appID, evaluation)
}

func (w *healthWatcher) newEvaluationLocked(epoch uint64) *healthEvaluation {
	// cancel is retained by healthEvaluation and called during finish or reset
	ctx, cancel := context.WithTimeout(w.ctx, healthEvaluateTimeout) //nolint:gosec
	return &healthEvaluation{
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
		epoch:  epoch,
	}
}

func (w *healthWatcher) runEvaluation(appID string, evaluation *healthEvaluation) {
	defer w.finishEvaluation(appID, evaluation)

	w.mu.Lock()
	current := !w.stopped && w.evaluating[appID] == evaluation && w.epochs[appID] == evaluation.epoch
	w.mu.Unlock()
	if !current {
		return
	}

	health := w.checkHealth(evaluation.ctx, appID)
	if health == HealthUnknown {
		return
	}

	w.mu.Lock()
	if w.stopped || w.evaluating[appID] != evaluation || w.epochs[appID] != evaluation.epoch {
		w.mu.Unlock()
		return
	}
	last, seen := w.lastReported[appID]
	if seen && last == health {
		w.mu.Unlock()
		return
	}
	w.lastReported[appID] = health
	sender := w.sender
	w.mu.Unlock()

	if sender == nil {
		return
	}
	if err := sender.SendMessage(&messages.ClientMessage{
		Payload: &messages.ClientMessage_ApplicationStatusReport{
			ApplicationStatusReport: &messages.ApplicationStatusReport{
				Statuses: []*messages.ApplicationStatus{{ApplicationId: appID, Health: health.Proto()}},
			},
		},
	}); err != nil {
		w.log.Error().Err(err).Str("application_id", appID).Msg("failed to send health report")
	}
}

func (w *healthWatcher) finishEvaluation(appID string, evaluation *healthEvaluation) {
	w.mu.Lock()
	var next *healthEvaluation
	if w.evaluating[appID] == evaluation && !w.stopped {
		if pending := w.pending[appID]; pending != nil && pending.ready && w.epochs[appID] == pending.epoch {
			delete(w.pending, appID)
			next = w.newEvaluationLocked(pending.epoch)
			w.evaluating[appID] = next
		}
	}

	if next == nil && w.evaluating[appID] == evaluation {
		delete(w.evaluating, appID)
		if w.pending[appID] == nil {
			delete(w.epochs, appID)
		}
	}
	w.mu.Unlock()

	evaluation.cancel()
	close(evaluation.done)
	if next != nil {
		go w.runEvaluation(appID, next)
	}
}
