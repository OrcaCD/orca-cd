package docker

import (
	"context"
	"testing"
	"time"

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
			name:  "exited container is unhealthy",
			items: []container.Summary{{State: container.StateRunning}, {State: container.StateExited}},
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

func TestWaitForSettledHealth_ReturnsImmediatelyWhenSettled(t *testing.T) {
	calls := 0
	got := waitForSettledHealth(t.Context(), time.Hour, func() HealthState {
		calls++
		return HealthHealthy
	})
	if got != HealthHealthy {
		t.Errorf("expected HealthHealthy, got %v", got)
	}
	if calls != 1 {
		t.Errorf("expected a single check, got %d", calls)
	}
}

func TestWaitForSettledHealth_PollsUntilSettled(t *testing.T) {
	calls := 0
	got := waitForSettledHealth(t.Context(), time.Millisecond, func() HealthState {
		calls++
		if calls < 3 {
			return HealthUnknown
		}
		return HealthUnhealthy
	})
	if got != HealthUnhealthy {
		t.Errorf("expected HealthUnhealthy, got %v", got)
	}
	if calls != 3 {
		t.Errorf("expected 3 checks, got %d", calls)
	}
}

func TestWaitForSettledHealth_ReturnsUnknownOnTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	got := waitForSettledHealth(ctx, time.Millisecond, func() HealthState {
		return HealthUnknown
	})
	if got != HealthUnknown {
		t.Errorf("expected HealthUnknown when health never settles, got %v", got)
	}
}
