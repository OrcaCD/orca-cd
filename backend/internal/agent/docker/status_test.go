package docker

import (
	"testing"

	"github.com/moby/moby/api/types/container"
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

	c, err := New(zerolog.Nop(), t.TempDir(), "", nil, false)
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
			name:  "exited container is unhealthy",
			items: []container.Summary{{State: container.StateRunning}, {State: container.StateExited}},
			want:  HealthUnhealthy,
		},
		{
			name:  "running but unhealthy healthcheck is unhealthy",
			items: []container.Summary{{State: container.StateRunning, Health: &container.HealthSummary{Status: container.Unhealthy}}},
			want:  HealthUnhealthy,
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
