package websocket

import (
	"context"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

func seedAppForAgent(t *testing.T, agentID string) models.Application {
	t.Helper()
	app := models.Application{
		Name:         crypto.EncryptedString("app-" + agentID),
		AgentId:      agentID,
		SyncStatus:   models.UnknownSync,
		HealthStatus: models.UnknownHealth,
		Branch:       "main",
		Path:         "deploy.yml",
		ComposeFile:  crypto.EncryptedString("version: '3.9'\n"),
	}
	if err := db.DB.Select("*").Create(&app).Error; err != nil {
		t.Fatalf("failed to seed application: %v", err)
	}
	return app
}

func healthOf(t *testing.T, id string) models.HealthStatus {
	t.Helper()
	app, err := gorm.G[models.Application](db.DB).Where("id = ?", id).First(context.Background())
	if err != nil {
		t.Fatalf("load application %s: %v", id, err)
	}
	return app.HealthStatus
}

func TestHandleApplicationStatusReport_UpdatesOnlyReportingAgentApps(t *testing.T) {
	setupDeployTestEnv(t)

	appA := seedAppForAgent(t, "agent-1")
	appB := seedAppForAgent(t, "agent-1")
	other := seedAppForAgent(t, "agent-2")

	nop := zerolog.Nop()
	client := &Client{Id: "agent-1"}

	handleApplicationStatusReport(t.Context(), client, &messages.ApplicationStatusReport{
		Statuses: []*messages.ApplicationStatus{
			{ApplicationId: appA.Id, Health: messages.HealthStatus_HEALTH_STATUS_HEALTHY},
			{ApplicationId: appB.Id, Health: messages.HealthStatus_HEALTH_STATUS_UNHEALTHY},
			// Spoofed: belongs to a different agent — must be ignored.
			{ApplicationId: other.Id, Health: messages.HealthStatus_HEALTH_STATUS_HEALTHY},
		},
	}, &nop)

	if got := healthOf(t, appA.Id); got != models.Healthy {
		t.Errorf("appA: expected healthy, got %q", got)
	}
	if got := healthOf(t, appB.Id); got != models.Unhealthy {
		t.Errorf("appB: expected unhealthy, got %q", got)
	}
	if got := healthOf(t, other.Id); got != models.UnknownHealth {
		t.Errorf("other agent's app must be untouched, got %q", got)
	}
}

func setAppHealthAndSync(t *testing.T, id string, health models.HealthStatus, sync models.SyncStatus) {
	t.Helper()
	if _, err := gorm.G[models.Application](db.DB).
		Where("id = ?", id).
		Updates(t.Context(), models.Application{HealthStatus: health, SyncStatus: sync}); err != nil {
		t.Fatalf("failed to set application health: %v", err)
	}
}

func TestApplyReportedHealth_Transitions(t *testing.T) {
	tests := []struct {
		name       string
		health     models.HealthStatus
		sync       models.SyncStatus
		reported   models.HealthStatus
		wantEvent  models.NotificationEvent
		wantHealth models.HealthStatus
	}{
		{"healthy to unhealthy", models.Healthy, models.Synced, models.Unhealthy, models.NotificationEventHealthUnhealthy, models.Unhealthy},
		{"healthy to unhealthy while deploying", models.Healthy, models.Syncing, models.Unhealthy, "", models.Unhealthy},
		{"unknown to unhealthy after reconnect", models.UnknownHealth, models.Synced, models.Unhealthy, "", models.Unhealthy},
		{"still unhealthy", models.Unhealthy, models.Synced, models.Unhealthy, "", models.Unhealthy},
		{"unhealthy to healthy", models.Unhealthy, models.Synced, models.Healthy, models.NotificationEventHealthRecovered, models.Healthy},
		{"unknown to healthy", models.UnknownHealth, models.Synced, models.Healthy, "", models.Healthy},
		{"still healthy", models.Healthy, models.Synced, models.Healthy, "", models.Healthy},
		{"healthy to unknown", models.Healthy, models.Synced, models.UnknownHealth, "", models.UnknownHealth},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupDeployTestEnv(t)
			app := seedAppForAgent(t, "agent-1")
			setAppHealthAndSync(t, app.Id, tt.health, tt.sync)

			event, err := applyReportedHealth(t.Context(), "agent-1", app.Id, tt.reported)
			if err != nil {
				t.Fatalf("applyReportedHealth() error: %v", err)
			}
			if event != tt.wantEvent {
				t.Errorf("expected event %q, got %q", tt.wantEvent, event)
			}
			if got := healthOf(t, app.Id); got != tt.wantHealth {
				t.Errorf("expected health %q, got %q", tt.wantHealth, got)
			}

			// Repeating the same report is never a transition.
			event, err = applyReportedHealth(t.Context(), "agent-1", app.Id, tt.reported)
			if err != nil {
				t.Fatalf("applyReportedHealth() error: %v", err)
			}
			if event != "" {
				t.Errorf("expected repeated report to be silent, got %q", event)
			}
		})
	}
}

func TestApplyReportedHealth_IgnoresOtherAgentsApps(t *testing.T) {
	setupDeployTestEnv(t)
	app := seedAppForAgent(t, "agent-1")
	setAppHealthAndSync(t, app.Id, models.Healthy, models.Synced)

	event, err := applyReportedHealth(t.Context(), "agent-2", app.Id, models.Unhealthy)
	if err != nil {
		t.Fatalf("applyReportedHealth() error: %v", err)
	}
	if event != "" {
		t.Errorf("expected no event for another agent's app, got %q", event)
	}
	if got := healthOf(t, app.Id); got != models.Healthy {
		t.Errorf("expected health to stay healthy, got %q", got)
	}
}

func TestHandleApplicationStatusReport_NotifiesOnHealthTransition(t *testing.T) {
	setupDeployTestEnv(t)
	app := seedAppForAgent(t, "agent-1")
	setAppHealthAndSync(t, app.Id, models.Healthy, models.Synced)
	expectAsyncNotification(t)

	nop := zerolog.Nop()
	handleApplicationStatusReport(t.Context(), &Client{Id: "agent-1"}, &messages.ApplicationStatusReport{
		Statuses: []*messages.ApplicationStatus{
			{ApplicationId: app.Id, Health: messages.HealthStatus_HEALTH_STATUS_UNHEALTHY},
		},
	}, &nop)

	if got := healthOf(t, app.Id); got != models.Unhealthy {
		t.Errorf("expected unhealthy, got %q", got)
	}
}
