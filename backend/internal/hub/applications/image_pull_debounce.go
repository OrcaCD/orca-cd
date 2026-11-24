package applications

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

const (
	// imagePullDebounceWindow is the quiet period a burst of image update triggers
	// has to settle for before the pull is dispatched. One registry push can fan out
	// into a delivery per manifest of a multi-arch image.
	imagePullDebounceWindow = 5 * time.Second
	// imagePullDebounceMaxDelay caps how long a continuous stream of triggers can
	// postpone the pull.
	imagePullDebounceMaxDelay = 30 * time.Second
)

// DefaultImagePullDebouncer coalesces image update webhook deliveries. It is nil
// until the hub initializes it, in which case pulls are dispatched immediately.
var DefaultImagePullDebouncer *ImagePullDebouncer

type pendingImagePull struct {
	timer *time.Timer
	// deadline is the hard cap, measured from the first trigger of the burst.
	deadline time.Time
	source   models.ApplicationEventSource
	// images are the images the triggers announced, if they all named one.
	images []string
	// unrestricted means a trigger named no image, so every image is updated.
	unrestricted bool
	// next holds the triggers that arrived after the deadline and that this
	// pull doesn't cover. They are queued once it is dispatched.
	next *pendingImagePull
}

// covers reports whether the pull updates the image a trigger announced.
func (p *pendingImagePull) covers(image string) bool {
	return p.unrestricted || (image != "" && slices.Contains(p.images, image))
}

// add merges a trigger into the pull.
func (p *pendingImagePull) add(source models.ApplicationEventSource, image string) {
	p.source = source
	p.unrestricted = p.unrestricted || image == ""
	if p.unrestricted {
		p.images = nil
	} else {
		p.images = appendImage(p.images, image)
	}
}

// ImagePullDebouncer collapses bursts of image update triggers for the same
// application into one pull, so one push yields one history entry and one notification.
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
// right away when no debouncer is initialized. image is the image the trigger
// announced, or empty if it named none.
func ScheduleImagePull(app *models.Application, source models.ApplicationEventSource, image string) {
	if DefaultImagePullDebouncer == nil {
		TriggerImagePull(app, SyncOrigin{Source: source, WebhookImages: appendImage(nil, image)})
		return
	}
	DefaultImagePullDebouncer.Schedule(app.Id, source, image)
}

func appendImage(images []string, image string) []string {
	if image == "" || slices.Contains(images, image) {
		return images
	}
	return append(images, image)
}

// Schedule queues a pull for appID once no further trigger arrives within the
// debounce window, but never later than maxDelay after the first trigger.
func (d *ImagePullDebouncer) Schedule(appID string, source models.ApplicationEventSource, image string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped {
		return
	}

	now := time.Now()
	pending, ok := d.pending[appID]
	switch {
	case !ok:
		pending = &pendingImagePull{deadline: now.Add(d.maxDelay)}
	case !now.Before(pending.deadline):
		// Hard cap elapsed; the queued pull is already on its way out.
		if !pending.covers(image) {
			if pending.next == nil {
				pending.next = &pendingImagePull{}
			}
			pending.next.add(source, image)
		}
		return
	default:
		pending.timer.Stop()
		pending = &pendingImagePull{deadline: pending.deadline, source: pending.source, images: pending.images, unrestricted: pending.unrestricted}
	}
	pending.add(source, image)
	d.queue(appID, pending, now)
}

// queue starts the timer of pending. The caller must hold d.mu.
func (d *ImagePullDebouncer) queue(appID string, pending *pendingImagePull, now time.Time) {
	delay := min(d.window, pending.deadline.Sub(now))
	pending.timer = time.AfterFunc(delay, func() { d.dispatch(appID, pending) })
	d.pending[appID] = pending

	d.log.Debug().Str("applicationId", appID).Dur("delay", delay).Msg("image pull scheduled")
}

// Cancel drops a queued pull for appID, used when a pull is dispatched elsewhere.
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

// dispatch reloads the application, which may have changed or been deleted while
// the burst settled, and triggers the pull.
func (d *ImagePullDebouncer) dispatch(appID string, pending *pendingImagePull) {
	d.mu.Lock()
	if d.stopped || d.pending[appID] != pending {
		d.mu.Unlock()
		return
	}
	delete(d.pending, appID)
	if next := pending.next; next != nil {
		now := time.Now()
		next.deadline = now.Add(d.maxDelay)
		d.queue(appID, next, now)
	}
	d.wg.Add(1)
	d.mu.Unlock()
	defer d.wg.Done()

	if db.DB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	app, err := gorm.G[models.Application](db.DB).Where("id = ?", appID).First(ctx)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			d.log.Error().Err(err).Str("applicationId", appID).
				Msg("failed to load application for debounced image pull")
		}
		return
	}
	triggerImagePull(&app, SyncOrigin{Source: pending.source, WebhookImages: pending.images})
}
