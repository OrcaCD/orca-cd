package docker

import (
	"context"

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
			// TODO: Expose a distinct starting state once the hub protocol and
			// application health model can represent it end to end.
			settling = true
		}
	}

	if settling {
		return HealthUnknown
	}
	return HealthHealthy
}
