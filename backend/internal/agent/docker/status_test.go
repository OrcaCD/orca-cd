package docker

import (
	"context"
	"errors"
	"slices"
	"testing"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/rs/zerolog"
)

func TestApplicationHealth_NoContainersIsUnknown(t *testing.T) {
	c := newTestClient(t)

	// No application has ever been deployed with this id, so no containers carry
	// its label.
	if got := c.ApplicationHealth(t.Context(), "no-such-application-id"); got != HealthUnknown {
		t.Errorf("expected HealthUnknown for an app with no containers, got %v", got)
	}
}

func TestApplicationHealth_DaemonUnreachableIsUnknown(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://localhost:1")

	c, err := New(zerolog.Nop(), t.TempDir(), nil, false)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(c.Close)

	if got := c.ApplicationHealth(t.Context(), "any"); got != HealthUnknown {
		t.Errorf("expected HealthUnknown when daemon is unreachable, got %v", got)
	}
}

func TestAggregateHealth(t *testing.T) {
	tests := []struct {
		name  string
		items []container.Summary
		want  HealthState
	}{
		{
			name:  "no containers is unknown",
			items: nil,
			want:  HealthUnknown,
		},
		{
			name:  "all running and no healthcheck is healthy",
			items: []container.Summary{{State: container.StateRunning}, {State: container.StateRunning}},
			want:  HealthHealthy,
		},
		{
			name:  "running and healthy is healthy",
			items: []container.Summary{{State: container.StateRunning, Health: &container.HealthSummary{Status: container.Healthy}}},
			want:  HealthHealthy,
		},
		{
			name:  "partially stopped is unhealthy",
			items: []container.Summary{{State: container.StateRunning}, {State: container.StateExited}},
			want:  HealthUnhealthy,
		},
		{
			name:  "all stopped is stopped",
			items: []container.Summary{{State: container.StateExited}, {State: container.StateExited}},
			want:  HealthStopped,
		},
		{
			name:  "created and paused containers are stopped",
			items: []container.Summary{{State: container.StateCreated}, {State: container.StatePaused}},
			want:  HealthStopped,
		},
		{
			name:  "restarting container is unhealthy",
			items: []container.Summary{{State: container.StateExited}, {State: container.StateRestarting}},
			want:  HealthUnhealthy,
		},
		{
			name:  "dead container is unhealthy",
			items: []container.Summary{{State: container.StateDead}},
			want:  HealthUnhealthy,
		},
		{
			name:  "running but unhealthy healthcheck is unhealthy",
			items: []container.Summary{{State: container.StateRunning, Health: &container.HealthSummary{Status: container.Unhealthy}}},
			want:  HealthUnhealthy,
		},
		{
			name:  "healthcheck still starting is unknown",
			items: []container.Summary{{State: container.StateRunning, Health: &container.HealthSummary{Status: container.Starting}}},
			want:  HealthUnknown,
		},
		{
			name: "starting healthcheck does not mask an unhealthy container",
			items: []container.Summary{
				{State: container.StateRunning, Health: &container.HealthSummary{Status: container.Starting}},
				{State: container.StateRunning, Health: &container.HealthSummary{Status: container.Unhealthy}},
			},
			want: HealthUnhealthy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := aggregateHealth(tt.items); got != tt.want {
				t.Errorf("aggregateHealth() = %v, want %v", got, tt.want)
			}
		})
	}
}

type fakeInspector struct {
	states map[string]*container.State
	err    error
	calls  []string
}

func (f *fakeInspector) ContainerInspect(_ context.Context, id string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	f.calls = append(f.calls, id)
	if f.err != nil {
		return client.ContainerInspectResult{}, f.err
	}
	return client.ContainerInspectResult{Container: container.InspectResponse{ID: id, State: f.states[id]}}, nil
}

func TestWithInspectedHealth(t *testing.T) {
	inspector := &fakeInspector{states: map[string]*container.State{
		"failing":        {Health: &container.Health{Status: container.Unhealthy, FailingStreak: 3}},
		"passing":        {Health: &container.Health{Status: container.Healthy}},
		"no-healthcheck": {},
	}}
	items := []container.Summary{
		{ID: "failing", State: container.StateRunning},
		{ID: "passing", State: container.StateRunning},
		{ID: "no-healthcheck", State: container.StateRunning},
		{ID: "listed", State: container.StateRunning, Health: &container.HealthSummary{Status: container.Healthy}},
		{ID: "stopped", State: container.StateExited},
	}

	got, err := withInspectedHealth(t.Context(), inspector, items)
	if err != nil {
		t.Fatalf("withInspectedHealth: %v", err)
	}

	if got[0].Health == nil || got[0].Health.Status != container.Unhealthy || got[0].Health.FailingStreak != 3 {
		t.Errorf("failing container health = %+v, want unhealthy with streak 3", got[0].Health)
	}
	if got[1].Health == nil || got[1].Health.Status != container.Healthy {
		t.Errorf("passing container health = %+v, want healthy", got[1].Health)
	}
	if got[2].Health != nil {
		t.Errorf("container without healthcheck got health %+v", got[2].Health)
	}
	if want := []string{"failing", "passing", "no-healthcheck"}; !slices.Equal(inspector.calls, want) {
		t.Errorf("inspected %v, want %v (listed and stopped containers must be skipped)", inspector.calls, want)
	}
	if items[0].Health != nil {
		t.Error("input items were modified")
	}
	if aggregateHealth(got) != HealthUnhealthy {
		t.Error("an unhealthy healthcheck only visible through inspect must make the application unhealthy")
	}
}

func TestWithInspectedHealth_InspectErrorIsReturned(t *testing.T) {
	inspector := &fakeInspector{err: errors.New("boom")}

	_, err := withInspectedHealth(t.Context(), inspector, []container.Summary{{ID: "a", State: container.StateRunning}})
	if err == nil {
		t.Fatal("expected an error when inspecting fails")
	}
}

func TestHealthStateProto(t *testing.T) {
	tests := map[HealthState]messages.HealthStatus{
		HealthUnknown:   messages.HealthStatus_HEALTH_STATUS_UNSPECIFIED,
		HealthHealthy:   messages.HealthStatus_HEALTH_STATUS_HEALTHY,
		HealthUnhealthy: messages.HealthStatus_HEALTH_STATUS_UNHEALTHY,
		HealthStopped:   messages.HealthStatus_HEALTH_STATUS_STOPPED,
	}
	for state, want := range tests {
		if got := state.Proto(); got != want {
			t.Errorf("HealthState(%d).Proto() = %v, want %v", state, got, want)
		}
	}
}
