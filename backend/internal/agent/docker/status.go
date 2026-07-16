package docker

import (
	"context"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// HealthState is the agent's view of an application's runtime health, derived
// from the state of its containers.
type HealthState int

const (
	HealthUnknown HealthState = iota
	HealthHealthy
	HealthUnhealthy
)

// HealthSettleTimeout bounds how long post-deploy health monitoring waits for
// an application's healthchecks to settle before reporting the current state.
const HealthSettleTimeout = 2 * time.Minute

const healthSettlePollInterval = 3 * time.Second

// Proto converts the health state to its protobuf representation.
func (s HealthState) Proto() messages.HealthStatus {
	switch s {
	case HealthHealthy:
		return messages.HealthStatus_HEALTH_STATUS_HEALTHY
	case HealthUnhealthy:
		return messages.HealthStatus_HEALTH_STATUS_UNHEALTHY
	default:
		return messages.HealthStatus_HEALTH_STATUS_UNSPECIFIED
	}
}

// ApplicationHealth reports the aggregate health of the containers belonging to
// an application, identified by the orca-cd.application-id label applied at
// deploy time. It returns HealthUnknown when the daemon is unreachable, no
// containers exist (nothing deployed yet), or a healthcheck is still starting.
// Any container that is not running, or whose healthcheck reports unhealthy,
// makes the whole application unhealthy.
func (c *Client) ApplicationHealth(ctx context.Context, appID string) HealthState {
	if !c.Ready() {
		return HealthUnknown
	}

	result, err := c.cli.Client().ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: make(client.Filters).Add("label", labelApplicationID+"="+appID),
	})
	if err != nil {
		c.log.Error().Err(err).Str("application_id", appID).Msg("failed to list containers for health check")
		return HealthUnknown
	}

	return aggregateHealth(result.Items)
}

// WaitForApplicationHealth polls the application's containers until their
// aggregate health settles to healthy or unhealthy, or ctx expires. Deployments
// no longer block on healthchecks, so this is how health is observed after the
// containers have been started. Returns the last observed state (HealthUnknown
// if it never settled).
func (c *Client) WaitForApplicationHealth(ctx context.Context, appID string) HealthState {
	return waitForSettledHealth(ctx, healthSettlePollInterval, func() HealthState {
		return c.ApplicationHealth(ctx, appID)
	})
}

func waitForSettledHealth(ctx context.Context, interval time.Duration, check func() HealthState) HealthState {
	state := check()
	if state != HealthUnknown {
		return state
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return state
		case <-ticker.C:
			if state = check(); state != HealthUnknown {
				return state
			}
		}
	}
}

// aggregateHealth derives an application's health from the state of its
// containers. No containers → Unknown (nothing deployed). Any container not
// running, or whose healthcheck is unhealthy, makes the application unhealthy.
// A healthcheck still starting keeps the application at Unknown until it
// settles.
func aggregateHealth(items []container.Summary) HealthState {
	if len(items) == 0 {
		return HealthUnknown
	}

	settling := false
	for _, item := range items {
		if item.State != container.StateRunning {
			return HealthUnhealthy
		}
		if item.Health == nil {
			continue
		}
		switch item.Health.Status {
		case container.Unhealthy:
			return HealthUnhealthy
		case container.Starting:
			settling = true
		}
	}

	if settling {
		return HealthUnknown
	}
	return HealthHealthy
}
