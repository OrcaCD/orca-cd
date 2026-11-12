package docker

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/rs/zerolog"
)

const minPollIntervalSeconds = 60

// PollSettings holds the image polling configuration for one application.
type PollSettings struct {
	Enabled         bool
	IntervalSeconds int64
	DeleteOldImages bool
	ReleaseAge      ReleaseAgePolicy
}

func (s PollSettings) equal(other PollSettings) bool {
	return s.Enabled == other.Enabled && s.IntervalSeconds == other.IntervalSeconds &&
		s.DeleteOldImages == other.DeleteOldImages && s.ReleaseAge.equal(other.ReleaseAge)
}

// AppPollConfig is the per-application input for ApplySettings.
type AppPollConfig struct {
	AppID    string
	AppName  string
	Settings PollSettings
}

// MessageSender is the minimal interface the poller needs to report results.
type MessageSender interface {
	SendMessage(msg *messages.ClientMessage) error
}

// checkAndPullImages is a package-level var so tests can override it.
var checkAndPullImages = func(ctx context.Context, c *Client, appID, appName string, deleteOld bool, policy ReleaseAgePolicy) (bool, []*messages.ImageUpdate, error) {
	return c.CheckAndPullImages(ctx, appID, appName, deleteOld, policy)
}

// listServiceImages is a package-level var so tests can override it.
var listServiceImages = func(ctx context.Context, c *Client, appID string) ([]*messages.ServiceImage, error) {
	return c.ServiceImages(ctx, appID)
}

type appPollState struct {
	settings PollSettings
	appName  string
	stop     chan struct{}
}

// ImagePoller manages per-application image polling tickers.
type ImagePoller struct {
	mu       sync.Mutex
	log      zerolog.Logger
	apps     map[string]*appPollState // keyed by applicationID
	runLocks map[string]*sync.Mutex
	failing  map[string]struct{} // applications whose last run failed
	heldBack map[string]string   // updates held back in the last run, by application
	client   *Client
	sender   MessageSender
	// intervalOverride replaces every application's interval when set, so the
	// E2E tests don't have to wait for the 60 second minimum.
	intervalOverride time.Duration
}

// NewImagePoller creates a new ImagePoller. sender is used to report results to the hub;
// SendMessage is a no-op when the agent is disconnected. A non-zero intervalOverride
// is used instead of the configured interval of every application.
func NewImagePoller(c *Client, sender MessageSender, log zerolog.Logger, intervalOverride time.Duration) *ImagePoller {
	return &ImagePoller{
		log:              log,
		apps:             make(map[string]*appPollState),
		runLocks:         make(map[string]*sync.Mutex),
		failing:          make(map[string]struct{}),
		heldBack:         make(map[string]string),
		client:           c,
		sender:           sender,
		intervalOverride: intervalOverride,
	}
}

// ApplySettings replaces the poller state with the given snapshot.
// Apps absent from the snapshot are stopped and removed, apps whose settings
// did not change keep their ticker.
func (p *ImagePoller) ApplySettings(apps []AppPollConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()

	incoming := make(map[string]struct{}, len(apps))
	for _, app := range apps {
		incoming[app.AppID] = struct{}{}
	}
	for appID, state := range p.apps {
		if _, keep := incoming[appID]; !keep {
			close(state.stop)
			delete(p.apps, appID)
		}
	}
	for _, app := range apps {
		settings := app.Settings
		if settings.IntervalSeconds < minPollIntervalSeconds {
			settings.IntervalSeconds = minPollIntervalSeconds
		}
		existing, ok := p.apps[app.AppID]
		// The hub resends the settings after deployments. Restarting an unchanged
		// ticker would postpone its next poll by a full interval every time.
		if ok && settings.Enabled && existing.settings.equal(settings) && existing.appName == app.AppName {
			continue
		}
		if ok {
			close(existing.stop)
			delete(p.apps, app.AppID)
		}
		if !settings.Enabled {
			continue
		}
		state := &appPollState{
			settings: settings,
			appName:  app.AppName,
			stop:     make(chan struct{}),
		}
		p.apps[app.AppID] = state
		go p.runTicker(app.AppID, app.AppName, settings, state.stop)
	}
}

// SettingsFor returns a copy of the current settings for appID, or nil if not configured.
func (p *ImagePoller) SettingsFor(appID string) *PollSettings {
	p.mu.Lock()
	defer p.mu.Unlock()
	if state, ok := p.apps[appID]; ok {
		copy := state.settings
		return &copy
	}
	return nil
}

// TriggerNow performs an immediate image check for appID outside the normal tick cycle.
// deleteOld and policy come from the request rather than the poll settings, so they
// also apply to applications that don't have periodic polling enabled.
func (p *ImagePoller) TriggerNow(appID, appName, requestID string, deleteOld bool, policy ReleaseAgePolicy) {
	go p.runOnce(appID, appName, requestID, deleteOld, policy)
}

// StopAll stops all running tickers and clears the state map.
func (p *ImagePoller) StopAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for appID, state := range p.apps {
		close(state.stop)
		delete(p.apps, appID)
	}
}

func (p *ImagePoller) runTicker(appID, appName string, settings PollSettings, stop <-chan struct{}) {
	interval := time.Duration(settings.IntervalSeconds) * time.Second
	if p.intervalOverride > 0 {
		interval = p.intervalOverride
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			p.runOnce(appID, appName, "", settings.DeleteOldImages, settings.ReleaseAge)
		}
	}
}

func (p *ImagePoller) runOnce(appID, appName, requestID string, deleteOld bool, policy ReleaseAgePolicy) {
	runLock := p.lockForRun(appID)
	runLock.Lock()
	defer runLock.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	updated, images, err := checkAndPullImages(ctx, p.client, appID, appName, deleteOld, policy)
	if images != nil {
		p.sendImageUpdates(appID, images)
	}
	var heldBack []*messages.HeldBackImage
	for _, image := range images {
		if isHeldBack(image) {
			heldBack = append(heldBack, &messages.HeldBackImage{Image: image.Image, Reason: image.Reason})
		}
	}

	// The hub resets health to unknown when it dispatches an explicit pull. A
	// successful update already re-reports it in CheckAndPullImages; without an
	// update (nothing stale, or the pull failed) nothing else would.
	if requestID != "" && !updated && p.client != nil {
		p.client.observeApplicationHealth(appID)
	}

	// Unsolicited periodic ticks stay silent when nothing changed, to avoid
	// spamming history/notifications. Explicitly triggered requests (webhook,
	// manual, GitHub Actions) always report back, even with no change, so the
	// hub can complete the correlated event and clear the application's
	// syncing status. Otherwise a duplicate webhook delivery whose pull is a
	// no-op (the first delivery already pulled the image) leaves its event
	// stuck "running" and the application stuck "Syncing" until hub restart.
	// The first success after a failure is reported too: the hub left the
	// application out of sync and only a success result restores it. Held back
	// updates are reported when they change, so the history shows why an
	// update waits without an entry for every tick.
	wasFailing := p.markRun(appID, err != nil)
	heldBackChanged := p.markHeldBack(appID, heldBack)
	if requestID == "" && !updated && err == nil && !wasFailing && !heldBackChanged {
		return
	}

	result := &messages.PullImagesResult{
		RequestId:      requestID,
		ApplicationId:  appID,
		ImagesUpdated:  updated,
		HeldBackImages: heldBack,
	}
	if err != nil {
		result.Success = false
		result.ErrorMessage = err.Error()
		result.RedeployFailed = errors.Is(err, ErrRedeployFailed)

		p.log.Error().Err(err).Str("application_id", appID).Msg("image poll: failed to check/pull images")
	} else {
		result.Success = true
		if updated {
			result.Images = p.serviceImages(ctx, appID)
		}
	}

	if sendErr := p.sender.SendMessage(&messages.ClientMessage{
		Payload: &messages.ClientMessage_PullImagesResult{
			PullImagesResult: result,
		},
	}); sendErr != nil {
		p.log.Error().Err(sendErr).Str("application_id", appID).Msg("image poll: failed to send result")
	}
}

// serviceImages lists the images the application runs after an update, so
// the hub can roll back to them. Failing to list them doesn't fail the update.
func (p *ImagePoller) serviceImages(ctx context.Context, appID string) []*messages.ServiceImage {
	if p.client == nil {
		return nil
	}
	images, err := listServiceImages(ctx, p.client, appID)
	if err != nil {
		p.log.Warn().Err(err).Str("application_id", appID).Msg("image poll: could not list service images")
		return nil
	}
	return images
}

// markRun records the outcome of a run and reports whether the previous run
// of the application had failed.
func (p *ImagePoller) markRun(appID string, failed bool) (previouslyFailed bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	_, previouslyFailed = p.failing[appID]
	if failed {
		p.failing[appID] = struct{}{}
	} else {
		delete(p.failing, appID)
	}
	return previouslyFailed
}

// sendImageUpdates reports the update state of every image after each check,
// so the hub can show upcoming updates and when the last check ran.
func (p *ImagePoller) sendImageUpdates(appID string, images []*messages.ImageUpdate) {
	if err := p.sender.SendMessage(&messages.ClientMessage{
		Payload: &messages.ClientMessage_ImageUpdatesReport{
			ImageUpdatesReport: &messages.ImageUpdatesReport{
				ApplicationId: appID,
				CheckedAt:     time.Now().Unix(),
				Images:        images,
			},
		},
	}); err != nil {
		p.log.Error().Err(err).Str("application_id", appID).Msg("image poll: failed to send image updates")
	}
}

// markHeldBack records the updates held back in a run and reports whether
// they changed since the previous run. Updates that are no longer held back
// are no change: they were either applied, which is reported, or vanished.
func (p *ImagePoller) markHeldBack(appID string, heldBack []*messages.HeldBackImage) (changed bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	var summary strings.Builder
	for _, image := range heldBack {
		summary.WriteString(image.Image + ": " + image.Reason + "\n")
	}
	previous := p.heldBack[appID]
	if len(heldBack) == 0 {
		delete(p.heldBack, appID)
		return false
	}
	p.heldBack[appID] = summary.String()
	return summary.String() != previous
}

func (p *ImagePoller) lockForRun(appID string) *sync.Mutex {
	p.mu.Lock()
	defer p.mu.Unlock()

	runLock, ok := p.runLocks[appID]
	if !ok {
		runLock = &sync.Mutex{}
		p.runLocks[appID] = runLock
	}
	return runLock
}
