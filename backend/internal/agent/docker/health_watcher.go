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
	reportMu     sync.Mutex
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

// ReportApplicationStatus sends a current health snapshot for appIDs. Snapshot
// and event-driven reports share one serialization point so an older snapshot
// cannot overtake a newer incremental report.
func (c *Client) ReportApplicationStatus(ctx context.Context, sender MessageSender, appIDs []string) {
	c.healthWatcher.reportApplicationStatus(ctx, sender, appIDs)
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
	evaluations := make(map[*healthEvaluation]struct{}, 2)

	w.mu.Lock()
	if evaluation := w.resetLocked(appID); evaluation != nil {
		evaluations[evaluation] = struct{}{}
	}
	w.mu.Unlock()

	// A report already in its check/send phase owns reportMu. The first reset
	// above invalidates its epoch; this barrier waits for it to observe that
	// invalidation, then clears any work queued while the barrier was pending.
	w.reportMu.Lock()
	w.mu.Lock()
	if evaluation := w.resetLocked(appID); evaluation != nil {
		evaluations[evaluation] = struct{}{}
	}
	w.mu.Unlock()
	w.reportMu.Unlock()

	for evaluation := range evaluations {
		<-evaluation.done
	}
}

// resetLocked clears the tracked state for appID and cancels its active
// evaluation. The caller must hold w.mu.
func (w *healthWatcher) resetLocked(appID string) *healthEvaluation {
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
	return evaluation
}

// stop prevents future evaluations, cancels pending and running work, and
// waits for active evaluations to finish.
func (w *healthWatcher) stop() {
	if w == nil {
		return
	}
	evaluations := make(map[*healthEvaluation]struct{})
	w.mu.Lock()
	w.stopLocked(evaluations)
	w.mu.Unlock()

	w.reportMu.Lock()
	w.mu.Lock()
	w.stopLocked(evaluations)
	w.mu.Unlock()
	w.reportMu.Unlock()

	for evaluation := range evaluations {
		<-evaluation.done
	}
}

func (w *healthWatcher) stopLocked(evaluations map[*healthEvaluation]struct{}) {
	w.stopped = true
	for appID, pending := range w.pending {
		pending.timer.Stop()
		delete(w.pending, appID)
	}
	clear(w.epochs)
	for _, evaluation := range w.evaluating {
		evaluation.cancel()
		evaluations[evaluation] = struct{}{}
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
	ctx, cancel := context.WithCancel(w.ctx) //nolint:gosec // cancel is retained by healthEvaluation
	return &healthEvaluation{
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
		epoch:  epoch,
	}
}

func (w *healthWatcher) runEvaluation(appID string, evaluation *healthEvaluation) {
	defer w.finishEvaluation(appID, evaluation)
	w.reportMu.Lock()
	defer w.reportMu.Unlock()

	w.mu.Lock()
	current := !w.stopped && w.evaluating[appID] == evaluation && w.epochs[appID] == evaluation.epoch
	w.mu.Unlock()
	if !current {
		return
	}

	checkCtx, cancel := context.WithTimeout(evaluation.ctx, healthEvaluateTimeout)
	health := w.checkHealth(checkCtx, appID)
	cancel()
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

func (w *healthWatcher) reportApplicationStatus(ctx context.Context, sender MessageSender, appIDs []string) {
	if w == nil || sender == nil || len(appIDs) == 0 {
		return
	}

	w.reportMu.Lock()
	defer w.reportMu.Unlock()
	w.mu.Lock()
	stopped := w.stopped
	w.mu.Unlock()
	if stopped {
		return
	}

	snapshotCtx, cancel := context.WithTimeout(ctx, healthEvaluateTimeout)
	defer cancel()
	statuses := make([]*messages.ApplicationStatus, 0, len(appIDs))
	reported := make(map[string]HealthState, len(appIDs))
	for _, appID := range appIDs {
		health := w.checkHealth(snapshotCtx, appID)
		statuses = append(statuses, &messages.ApplicationStatus{ApplicationId: appID, Health: health.Proto()})
		reported[appID] = health
	}
	w.mu.Lock()
	stopped = w.stopped
	w.mu.Unlock()
	if stopped || snapshotCtx.Err() != nil {
		return
	}

	if err := sender.SendMessage(&messages.ClientMessage{
		Payload: &messages.ClientMessage_ApplicationStatusReport{
			ApplicationStatusReport: &messages.ApplicationStatusReport{Statuses: statuses},
		},
	}); err != nil {
		w.log.Error().Err(err).Msg("failed to send application status snapshot")
		return
	}

	w.mu.Lock()
	if !w.stopped {
		for appID, health := range reported {
			if health == HealthUnknown {
				delete(w.lastReported, appID)
				continue
			}
			w.lastReported[appID] = health
		}
	}
	w.mu.Unlock()
}

// reconcileMissingApplication rechecks an application that was absent from a
// container-list snapshot before reporting Unknown. Compose recreate can make
// that snapshot stale while a replacement container is already running.
func (w *healthWatcher) reconcileMissingApplication(appID string) {
	if w == nil {
		return
	}
	evaluations := make(map[*healthEvaluation]struct{}, 2)
	w.mu.Lock()
	previous, previouslyReported := w.lastReported[appID]
	if evaluation := w.resetLocked(appID); evaluation != nil {
		evaluations[evaluation] = struct{}{}
	}
	w.mu.Unlock()

	w.reportMu.Lock()
	w.mu.Lock()
	if evaluation := w.resetLocked(appID); evaluation != nil {
		evaluations[evaluation] = struct{}{}
	}
	stopped := w.stopped
	sender := w.sender
	w.mu.Unlock()
	reported := false
	reportedHealth := HealthUnknown
	if !stopped && sender != nil {
		checkCtx, cancel := context.WithTimeout(w.ctx, healthEvaluateTimeout)
		health := w.checkHealth(checkCtx, appID)
		checkErr := checkCtx.Err()
		cancel()
		if checkErr == nil {
			if err := sender.SendMessage(&messages.ClientMessage{
				Payload: &messages.ClientMessage_ApplicationStatusReport{
					ApplicationStatusReport: &messages.ApplicationStatusReport{
						Statuses: []*messages.ApplicationStatus{{ApplicationId: appID, Health: health.Proto()}},
					},
				},
			}); err != nil {
				w.log.Error().Err(err).Str("application_id", appID).Msg("failed to reconcile missing application")
			} else {
				reported = true
				reportedHealth = health
			}
		}
	}
	w.mu.Lock()
	if !w.stopped {
		switch {
		case reported && reportedHealth != HealthUnknown:
			w.lastReported[appID] = reportedHealth
		case reported:
			delete(w.lastReported, appID)
		case previouslyReported:
			// Keep the app eligible for the next resync when validation or
			// delivery failed; otherwise the hub could retain stale health forever.
			w.lastReported[appID] = previous
		}
	}
	w.mu.Unlock()
	w.reportMu.Unlock()

	for evaluation := range evaluations {
		<-evaluation.done
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
