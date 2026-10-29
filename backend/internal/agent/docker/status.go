package docker

import (
	"context"
	"fmt"
	"slices"
	"strings"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/docker/compose/v5/pkg/api"
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
	HealthStopped
)

// Proto converts the health state to its protobuf representation.
func (s HealthState) Proto() messages.HealthStatus {
	switch s {
	case HealthHealthy:
		return messages.HealthStatus_HEALTH_STATUS_HEALTHY
	case HealthUnhealthy:
		return messages.HealthStatus_HEALTH_STATUS_UNHEALTHY
	case HealthStopped:
		return messages.HealthStatus_HEALTH_STATUS_STOPPED
	default:
		return messages.HealthStatus_HEALTH_STATUS_UNSPECIFIED
	}
}

// ServiceHealth is the health of a single compose service of an application.
type ServiceHealth struct {
	Name  string
	State HealthState
}

// HealthReport is an application's aggregate health together with the health
// of each of its compose services, sorted by name.
type HealthReport struct {
	State    HealthState
	Services []ServiceHealth
}

// Equal reports whether both reports describe the same aggregate and service
// health.
func (r HealthReport) Equal(other HealthReport) bool {
	return r.State == other.State && slices.Equal(r.Services, other.Services)
}

// Proto converts the report to the status message sent to the hub.
func (r HealthReport) Proto(appID string) *messages.ApplicationStatus {
	status := &messages.ApplicationStatus{ApplicationId: appID, Health: r.State.Proto()}
	for _, service := range r.Services {
		status.Services = append(status.Services, &messages.ServiceStatus{Name: service.Name, Health: service.State.Proto()})
	}
	return status
}

// ApplicationHealth reports the health of the containers belonging to an
// application, identified by the orca-cd.application-id label applied at
// deploy time. The aggregate state is HealthUnknown when the daemon is
// unreachable, no containers exist (nothing deployed yet), or a healthcheck is
// still starting. HealthStopped is returned when none of its containers run.
func (c *Client) ApplicationHealth(ctx context.Context, appID string) HealthReport {
	if !c.Ready() {
		return HealthReport{State: HealthUnknown}
	}

	result, err := c.cli.Client().ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: make(client.Filters).Add("label", labelApplicationID+"="+appID),
	})
	if err != nil {
		c.log.Error().Err(err).Str("application_id", appID).Msg("failed to list containers for health check")
		return HealthReport{State: HealthUnknown}
	}

	items, err := withInspectedHealth(ctx, c.cli.Client(), result.Items)
	if err != nil {
		c.log.Error().Err(err).Str("application_id", appID).Msg("failed to inspect containers for health check")
		return HealthReport{State: HealthUnknown}
	}

	return HealthReport{State: aggregateHealth(items), Services: servicesHealth(items)}
}

// withInspectedHealth fills in the healthcheck summary of running containers
// that the container list left empty. Daemons older than Docker 28.4 don't
// return it in the list, so without the inspect a failing healthcheck would
// look like a container without one.
func withInspectedHealth(ctx context.Context, inspector containerInspector, items []container.Summary) ([]container.Summary, error) {
	resolved := slices.Clone(items)
	for i, item := range resolved {
		if item.State != container.StateRunning || item.Health != nil {
			continue
		}

		result, err := inspector.ContainerInspect(ctx, item.ID, client.ContainerInspectOptions{})
		if err != nil {
			return nil, fmt.Errorf("inspect container %q: %w", item.ID, err)
		}
		if state := result.Container.State; state != nil && state.Health != nil {
			resolved[i].Health = &container.HealthSummary{
				Status:        state.Health.Status,
				FailingStreak: state.Health.FailingStreak,
			}
		}
	}
	return resolved, nil
}

// aggregateHealth derives an application's health from the state of its
// containers. No containers → Unknown (nothing deployed). All containers
// stopped → Stopped. Any other container not running, or whose healthcheck is
// unhealthy, makes the application unhealthy. A healthcheck still starting
// keeps the application at Unknown until it settles.
func aggregateHealth(items []container.Summary) HealthState {
	if len(items) == 0 {
		return HealthUnknown
	}
	if allStopped(items) {
		return HealthStopped
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

// servicesHealth applies aggregateHealth to the containers of each compose
// service, so the hub can tell a partially failing application apart from one
// that is failing entirely.
func servicesHealth(items []container.Summary) []ServiceHealth {
	byService := make(map[string][]container.Summary)
	for _, item := range items {
		name := serviceName(item)
		byService[name] = append(byService[name], item)
	}

	services := make([]ServiceHealth, 0, len(byService))
	for name, serviceItems := range byService {
		services = append(services, ServiceHealth{Name: name, State: aggregateHealth(serviceItems)})
	}
	slices.SortFunc(services, func(a, b ServiceHealth) int { return strings.Compare(a.Name, b.Name) })
	return services
}

// serviceName returns the compose service a container belongs to, falling back
// to the container name for containers that lack the compose label.
func serviceName(item container.Summary) string {
	if name := item.Labels[api.ServiceLabel]; name != "" {
		return name
	}
	if len(item.Names) > 0 {
		return strings.TrimPrefix(item.Names[0], "/")
	}
	return ""
}

// allStopped reports whether none of the containers run, as after
// `docker compose stop` or `docker compose pause`. Restarting (crash loop),
// dead and removing containers are not considered stopped.
func allStopped(items []container.Summary) bool {
	for _, item := range items {
		switch item.State {
		case container.StateCreated, container.StateExited, container.StatePaused:
		default:
			return false
		}
	}
	return true
}
