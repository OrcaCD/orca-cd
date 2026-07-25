package applications

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

const (
	// imagePullDebounceWindow is the quiet period a burst of image update
	// triggers has to settle for before the pull is dispatched. Registries fan a
	// single push out into several deliveries — GHCR publishes one package event
	// per manifest of a multi-arch image — so pulling on the first delivery both
	// floods history and notifications and can race the tag update itself.
	imagePullDebounceWindow = 5 * time.Second
	// imagePullDebounceMaxDelay caps how long a continuous stream of triggers can
	// postpone the pull.
	imagePullDebounceMaxDelay = 30 * time.Second
	imagePullDebounceTimeout  = 10 * time.Second
)

// DefaultImagePullDebouncer coalesces image update webhook deliveries. It is nil
// until the hub initializes it, in which case pulls are dispatched immediately.
var DefaultImagePullDebouncer *ImagePullDebouncer

type pendingImagePull struct {
	timer *time.Timer
	// deadline is the hard cap, measured from the first trigger of the burst.
	deadline time.Time
	source   models.ApplicationEventSource
}

// ImagePullDebouncer collapses bursts of image update triggers for the same
// application into a single pull, so one registry push results in one history
// entry and one notification instead of one per webhook delivery.
type ImagePullDebouncer struct {
	mu       sync.Mutex
	wg       sync.WaitGroup // tracks in-flight dispatches
	log      *zerolog.Logger
	window   time.Duration
	maxDelay time.Duration
	pending  map[string]*pendingImagePull
	stopped  bool
}

func NewImagePullDebouncer(log *zerolog.Logger) *ImagePullDebouncer {
	return &ImagePullDebouncer{
		log:      log,
		window:   imagePullDebounceWindow,
		maxDelay: imagePullDebounceMaxDelay,
		pending:  make(map[string]*pendingImagePull),
	}
}

// ScheduleImagePull queues a debounced image pull for app, or dispatches it
// right away when no debouncer is initialized.
func ScheduleImagePull(app *models.Application, source models.ApplicationEventSource) {
	if DefaultImagePullDebouncer == nil {
		TriggerImagePull(app, source)
		return
	}
	DefaultImagePullDebouncer.Schedule(app.Id, source)
}

// Schedule queues a pull for appID once no further trigger arrives within the
// debounce window. Repeated triggers restart that window, but never postpone the
// pull beyond maxDelay from the first trigger of the burst.
func (d *ImagePullDebouncer) Schedule(appID string, source models.ApplicationEventSource) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped {
		return
	}

	now := time.Now()
	deadline := now.Add(d.maxDelay)
	if d.maxDelay <= 0 {
		deadline = now.Add(d.window)
	}
	if pending, ok := d.pending[appID]; ok {
		if !now.Before(pending.deadline) {
			// The hard cap elapsed, so the queued pull is already on its way out.
			return
		}
		deadline = pending.deadline
		pending.timer.Stop()
	}

	pending := &pendingImagePull{deadline: deadline, source: source}
	delay := max(time.Duration(0), min(d.window, deadline.Sub(now)))
	pending.timer = time.AfterFunc(delay, func() { d.dispatch(appID, pending) })
	d.pending[appID] = pending

	d.log.Debug().Str("applicationId", appID).Dur("delay", delay).Msg("image pull scheduled")
}

// Cancel drops a queued pull for appID. Callers that dispatch a pull through
// another path use this so the pending burst does not pull a second time.
func (d *ImagePullDebouncer) Cancel(appID string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if pending, ok := d.pending[appID]; ok {
		pending.timer.Stop()
		delete(d.pending, appID)
	}
}

// Stop drops all queued pulls and waits for in-flight dispatches to finish.
// A pull lost to shutdown is picked up by the next webhook delivery or poll.
func (d *ImagePullDebouncer) Stop() {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.stopped = true
	for appID, pending := range d.pending {
		pending.timer.Stop()
		delete(d.pending, appID)
	}
	d.mu.Unlock()
	d.wg.Wait()
}

// dispatch reloads the application before triggering, since it may have been
// changed or deleted while the burst was settling.
func (d *ImagePullDebouncer) dispatch(appID string, pending *pendingImagePull) {
	d.mu.Lock()
	if d.stopped || d.pending[appID] != pending {
		d.mu.Unlock()
		return
	}
	delete(d.pending, appID)
	d.wg.Add(1)
	d.mu.Unlock()
	defer d.wg.Done()

	if db.DB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), imagePullDebounceTimeout)
	defer cancel()

	app, err := gorm.G[models.Application](db.DB).Where("id = ?", appID).First(ctx)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			d.log.Error().Err(err).Str("applicationId", appID).
				Msg("failed to load application for debounced image pull")
		}
		return
	}
	TriggerImagePull(&app, pending.source)
}
