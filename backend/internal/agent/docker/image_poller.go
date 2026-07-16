package docker

import (
	"context"
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
var checkAndPullImages = func(ctx context.Context, c *Client, appID, appName string, deleteOld bool) (bool, error) {
	return c.CheckAndPullImages(ctx, appID, appName, deleteOld)
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
	client   *Client
	sender   MessageSender
}

// NewImagePoller creates a new ImagePoller. sender is used to report results to the hub;
// SendMessage is a no-op when the agent is disconnected.
func NewImagePoller(c *Client, sender MessageSender, log zerolog.Logger) *ImagePoller {
	return &ImagePoller{
		log:      log,
		apps:     make(map[string]*appPollState),
		runLocks: make(map[string]*sync.Mutex),
		client:   c,
		sender:   sender,
	}
}

// ApplySettings replaces the poller state with the given snapshot.
// Apps absent from the snapshot are stopped and removed.
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
		if existing, ok := p.apps[app.AppID]; ok {
			close(existing.stop)
			delete(p.apps, app.AppID)
		}
		settings := app.Settings
		if !settings.Enabled {
			continue
		}
		if settings.IntervalSeconds < minPollIntervalSeconds {
			settings.IntervalSeconds = minPollIntervalSeconds
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
func (p *ImagePoller) TriggerNow(appID, appName, requestID string) {
	go p.runOnce(appID, appName, requestID)
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
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			p.runOnce(appID, appName, "")
		}
	}
}

func (p *ImagePoller) runOnce(appID, appName, requestID string) {
	runLock := p.lockForRun(appID)
	runLock.Lock()
	defer runLock.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	p.mu.Lock()
	var deleteOld bool
	if state, ok := p.apps[appID]; ok {
		deleteOld = state.settings.DeleteOldImages
	}
	p.mu.Unlock()

	updated, err := checkAndPullImages(ctx, p.client, appID, appName, deleteOld)

	// Only notify the hub when something changed or an error occurred.
	if !updated && err == nil {
		return
	}

	result := &messages.PullImagesResult{
		RequestId:     requestID,
		ApplicationId: appID,
		ImagesUpdated: updated,
	}
	if err != nil {
		result.Success = false
		result.ErrorMessage = err.Error()

		p.log.Error().Err(err).Str("application_id", appID).Msg("image poll: failed to check/pull images")
	} else {
		result.Success = true
	}

	if sendErr := p.sender.SendMessage(&messages.ClientMessage{
		Payload: &messages.ClientMessage_PullImagesResult{
			PullImagesResult: result,
		},
	}); sendErr != nil {
		p.log.Error().Err(sendErr).Str("application_id", appID).Msg("image poll: failed to send result")
	}
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
