package docker

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/docker/compose/v5/pkg/compose"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
	"github.com/rs/zerolog"
)

const (
	daemonCheckInterval     = 5 * time.Second
	healthResyncInterval    = time.Minute
	hostDirDetectionTimeout = 5 * time.Second
)

type Client struct {
	mu                        sync.RWMutex // protects ready and host deployments directory state
	log                       zerolog.Logger
	cli                       command.Cli
	compose                   api.Compose
	deploymentsDir            string
	hostDeploymentsDir        string
	hostDirDetectionComplete  bool
	hostDirDetectionPending   chan struct{}
	detectHostDeploymentsDir  func(context.Context) (string, error)
	allowedPrivilegedApps     map[string]struct{}
	restrictMountsToDeployDir bool
	ready                     bool
	healthWatcher             *healthWatcher
	healthResyncMu            sync.Mutex
	ctx                       context.Context
	cancel                    context.CancelFunc
}

func New(log zerolog.Logger, deploymentsDir string, allowedPrivilegedApps map[string]struct{}, restrictMountsToDeployDir bool) (*Client, error) {
	dockerCLI, err := command.NewDockerCli()
	if err != nil {
		return nil, err
	}

	if err := dockerCLI.Initialize(&flags.ClientOptions{}); err != nil {
		return nil, err
	}

	composeSvc, err := compose.NewComposeService(dockerCLI)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{
		log:            log,
		cli:            dockerCLI,
		compose:        composeSvc,
		deploymentsDir: deploymentsDir,
		detectHostDeploymentsDir: func(ctx context.Context) (string, error) {
			return detectHostDeploymentsDir(ctx, dockerCLI.Client(), func() (string, error) {
				return detectContainerID(os.ReadFile)
			}, deploymentsDir)
		},
		allowedPrivilegedApps:     allowedPrivilegedApps,
		restrictMountsToDeployDir: restrictMountsToDeployDir,
		ctx:                       ctx,
		cancel:                    cancel,
	}
	c.healthWatcher = newHealthWatcher(c)

	if c.pingDaemon() {
		go c.startEventLoop()
		c.resyncApplicationHealth()
	} else {
		log.Warn().Msg("Docker daemon unreachable, will keep retrying in the background")
		go c.waitForDaemon()
	}
	go c.startHealthResyncLoop()

	return c, nil
}

func (c *Client) Close() {
	c.cancel()
	c.healthWatcher.stop()
}

func (c *Client) CLI() command.Cli {
	return c.cli
}

func (c *Client) Compose() api.Compose {
	return c.compose
}

// Reports whether the Docker daemon is currently reachable.
func (c *Client) Ready() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ready
}

func (c *Client) hostDeploymentsBase() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.hostDeploymentsDir
}

// resolveHostDeploymentsDir returns an error only when ctx is canceled before
// the detection state could be resolved; detection failures themselves are
// logged and retried on the next call.
func (c *Client) resolveHostDeploymentsDir(ctx context.Context) error {
	var detector func(context.Context) (string, error)
	for {
		var pending <-chan struct{}
		var shouldDetect bool
		detector, pending, shouldDetect = c.startHostDeploymentsDirDetection()
		if pending == nil {
			return nil
		}
		if shouldDetect {
			break
		}

		select {
		case <-pending:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	hostDeploymentsDir, err := runHostDeploymentsDirDetection(ctx, detector)
	c.finishHostDeploymentsDirDetection(hostDeploymentsDir, err)

	if errors.Is(err, errNotContainerized) {
		c.log.Debug().Msg("agent is running outside a container; using local deployment paths")
		return nil
	}
	if err != nil {
		if ctx.Err() != nil {
			return err
		}
		c.log.Debug().Err(err).Msg("could not auto-detect host deployments directory")
		return nil
	}
	c.log.Info().Str("host_deployments_dir", hostDeploymentsDir).Msg("auto-detected host deployments directory")
	return nil
}

func (c *Client) startHostDeploymentsDirDetection() (
	func(context.Context) (string, error),
	<-chan struct{},
	bool,
) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.hostDeploymentsDir != "" || c.hostDirDetectionComplete {
		return nil, nil, false
	}
	if c.hostDirDetectionPending != nil {
		return nil, c.hostDirDetectionPending, false
	}

	pending := make(chan struct{})
	c.hostDirDetectionPending = pending
	return c.detectHostDeploymentsDir, pending, true
}

func runHostDeploymentsDirDetection(
	ctx context.Context,
	detector func(context.Context) (string, error),
) (string, error) {
	if detector == nil {
		return "", errors.New("host deployments directory detector is not configured")
	}

	detectionCtx, cancel := context.WithTimeout(ctx, hostDirDetectionTimeout)
	defer cancel()
	return detector(detectionCtx)
}

func (c *Client) finishHostDeploymentsDirDetection(hostDeploymentsDir string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err == nil {
		c.hostDeploymentsDir = hostDeploymentsDir
		c.hostDirDetectionComplete = true
	} else if errors.Is(err, errNotContainerized) {
		c.hostDirDetectionComplete = true
	}
	pending := c.hostDirDetectionPending
	c.hostDirDetectionPending = nil
	close(pending)
}

func (c *Client) pingDaemon() bool {
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()

	ping, err := c.cli.Client().Ping(ctx, client.PingOptions{NegotiateAPIVersion: true})
	if err != nil {
		c.mu.Lock()
		wasReady := c.ready
		c.ready = false
		c.mu.Unlock()

		if wasReady {
			c.log.Error().Err(err).Msg("Docker daemon unreachable")
		}
		return false
	}
	if err := c.resolveHostDeploymentsDir(c.ctx); err != nil {
		c.log.Debug().Err(err).Msg("host deployments directory detection interrupted")
	}

	c.mu.Lock()
	wasReady := c.ready
	c.ready = true
	c.mu.Unlock()

	if !wasReady {
		c.log.Info().Str("api_version", ping.APIVersion).Msg("Docker daemon is reachable")
	}
	return true
}

// startEventLoop opens an event stream from the Docker daemon and dispatches
// incoming events. When the stream errors out (daemon went away), it hands off
// to waitForDaemon to poll for recovery.
func (c *Client) startEventLoop() {
	result := c.cli.Client().Events(c.ctx, client.EventsListOptions{})
	for {
		select {
		case <-c.ctx.Done():
			return
		case msg, ok := <-result.Messages:
			if !ok {
				return
			}
			c.handleEvent(msg)
		case err, ok := <-result.Err:
			if !ok {
				return
			}
			c.mu.Lock()
			c.ready = false
			c.mu.Unlock()
			c.log.Error().Err(err).Msg("Docker daemon unreachable")
			go c.waitForDaemon()
			return
		}
	}
}

func (c *Client) handleEvent(msg events.Message) {
	if msg.Type != events.ContainerEventType {
		return
	}
	appID := msg.Actor.Attributes[labelApplicationID]
	if appID == "" || !isHealthRelevantAction(msg.Action) {
		return
	}
	c.healthWatcher.schedule(appID)
}

// resyncApplicationHealth reconciles the watcher's state with the containers
// currently known to Docker, then re-evaluates each active application.
func (c *Client) resyncApplicationHealth() {
	if !c.Ready() {
		return
	}

	c.healthResyncMu.Lock()
	defer c.healthResyncMu.Unlock()
	if !c.Ready() {
		return
	}

	ctx, cancel := context.WithTimeout(c.ctx, healthEvaluateTimeout)
	defer cancel()
	result, err := c.cli.Client().ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: make(client.Filters).Add("label", labelApplicationID),
	})
	if err != nil {
		c.log.Error().Err(err).Msg("health resync: failed to list containers")
		return
	}

	appIDs := make(map[string]struct{})
	for _, item := range result.Items {
		if appID := item.Labels[labelApplicationID]; appID != "" {
			appIDs[appID] = struct{}{}
		}
	}
	c.reconcileApplicationHealth(appIDs)
}

func (c *Client) reconcileApplicationHealth(appIDs map[string]struct{}) {
	for appID := range c.healthWatcher.knownApps() {
		if _, active := appIDs[appID]; !active {
			c.healthWatcher.forget(appID)
		}
	}
	for appID := range appIDs {
		c.healthWatcher.schedule(appID)
	}
}

// startHealthResyncLoop periodically repairs missed event transitions and
// drops watcher state for applications whose containers disappeared outside
// OrcaCD, for example through a manual docker compose down.
func (c *Client) startHealthResyncLoop() {
	ticker := time.NewTicker(healthResyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.resyncApplicationHealth()
		}
	}
}

// waitForDaemon polls until the daemon is reachable again, then resumes the event loop.
func (c *Client) waitForDaemon() {
	ticker := time.NewTicker(daemonCheckInterval)
	defer ticker.Stop()
	var ticks int
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			ticks++
			if c.pingDaemon() {
				go c.startEventLoop()
				go c.resyncApplicationHealth()
				return
			}
			if ticks%12 == 0 {
				c.log.Warn().Msg("Docker daemon still unreachable, retrying in the background")
			}
		}
	}
}
