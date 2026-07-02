package docker

import (
	"context"

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

// ApplicationHealth reports the aggregate health of the containers belonging to
// an application, identified by the orca-cd.application-id label applied at
// deploy time. It returns HealthUnknown when the daemon is unreachable or no
// containers exist (nothing deployed yet). Any container that is not running, or
// whose healthcheck reports unhealthy, makes the whole application unhealthy.
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

// aggregateHealth derives an application's health from the state of its
// containers. No containers → Unknown (nothing deployed). Any container not
// running, or whose healthcheck is unhealthy, makes the application unhealthy.
func aggregateHealth(items []container.Summary) HealthState {
	if len(items) == 0 {
		return HealthUnknown
	}

	for _, item := range items {
		if item.State != container.StateRunning {
			return HealthUnhealthy
		}
		if item.Health != nil && item.Health.Status == container.Unhealthy {
			return HealthUnhealthy
		}
	}

	return HealthHealthy
}
