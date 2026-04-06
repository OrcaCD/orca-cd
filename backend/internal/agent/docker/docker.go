package docker

import (
	"context"
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

const daemonCheckInterval = 5 * time.Second

type Client struct {
	mu      sync.RWMutex // protects ready
	log     zerolog.Logger
	cli     command.Cli
	compose api.Compose
	ready   bool
	ctx     context.Context
	cancel  context.CancelFunc
}

func New(log zerolog.Logger) (*Client, error) {
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
		log:     log,
		cli:     dockerCLI,
		compose: composeSvc,
		ctx:     ctx,
		cancel:  cancel,
	}

	if c.pingDaemon() {
		go c.startEventLoop()
	} else {
		log.Warn().Msg("Docker daemon unreachable, will keep retrying in the background")
		go c.waitForDaemon()
	}

	return c, nil
}

func (c *Client) Close() {
	c.cancel()
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
	// TODO: We can handle certain events here in the future
	_ = msg
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
				return
			}
			if ticks%12 == 0 {
				c.log.Warn().Msg("Docker daemon still unreachable, retrying in the background")
			}
		}
	}
}
