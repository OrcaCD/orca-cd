package docker

import (
	"testing"

	"github.com/moby/moby/api/types/container"
)

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
