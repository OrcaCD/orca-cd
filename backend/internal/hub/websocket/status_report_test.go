package websocket

import (
	"context"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/healthhistory"
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
	appC := seedAppForAgent(t, "agent-1")
	other := seedAppForAgent(t, "agent-2")

	nop := zerolog.Nop()
	client := &Client{Id: "agent-1"}

	handleApplicationStatusReport(t.Context(), client, &messages.ApplicationStatusReport{
		Statuses: []*messages.ApplicationStatus{
			{ApplicationId: appA.Id, Health: messages.HealthStatus_HEALTH_STATUS_HEALTHY},
			{ApplicationId: appB.Id, Health: messages.HealthStatus_HEALTH_STATUS_UNHEALTHY},
			{ApplicationId: appC.Id, Health: messages.HealthStatus_HEALTH_STATUS_STOPPED},
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
	if got := healthOf(t, appC.Id); got != models.Stopped {
		t.Errorf("appC: expected stopped, got %q", got)
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
		{"healthy to stopped", models.Healthy, models.Synced, models.Stopped, models.NotificationEventHealthUnhealthy, models.Stopped},
		{"healthy to stopped while deploying", models.Healthy, models.Syncing, models.Stopped, "", models.Stopped},
		{"unknown to stopped after reconnect", models.UnknownHealth, models.Synced, models.Stopped, "", models.Stopped},
		{"unhealthy to stopped", models.Unhealthy, models.Synced, models.Stopped, "", models.Stopped},
		{"stopped to unhealthy", models.Stopped, models.Synced, models.Unhealthy, "", models.Unhealthy},
		{"stopped to healthy", models.Stopped, models.Synced, models.Healthy, models.NotificationEventHealthRecovered, models.Healthy},
		{"healthy to degraded", models.Healthy, models.Synced, models.Degraded, models.NotificationEventHealthUnhealthy, models.Degraded},
		{"degraded to unhealthy", models.Degraded, models.Synced, models.Unhealthy, "", models.Unhealthy},
		{"unhealthy to degraded", models.Unhealthy, models.Synced, models.Degraded, "", models.Degraded},
		{"degraded to healthy", models.Degraded, models.Synced, models.Healthy, models.NotificationEventHealthRecovered, models.Healthy},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupDeployTestEnv(t)
			app := seedAppForAgent(t, "agent-1")
			setAppHealthAndSync(t, app.Id, tt.health, tt.sync)

			event, _, err := applyReportedHealth(t.Context(), "agent-1", app.Id, tt.reported)
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
			event, _, err = applyReportedHealth(t.Context(), "agent-1", app.Id, tt.reported)
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

	event, _, err := applyReportedHealth(t.Context(), "agent-2", app.Id, models.Unhealthy)
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

func TestReportedApplicationHealth(t *testing.T) {
	healthy := messages.HealthStatus_HEALTH_STATUS_HEALTHY
	unhealthy := messages.HealthStatus_HEALTH_STATUS_UNHEALTHY
	stopped := messages.HealthStatus_HEALTH_STATUS_STOPPED
	service := func(name string, health messages.HealthStatus) *messages.ServiceStatus {
		return &messages.ServiceStatus{Name: name, Health: health}
	}

	tests := []struct {
		name   string
		status *messages.ApplicationStatus
		want   models.HealthStatus
	}{
		{"healthy", &messages.ApplicationStatus{Health: healthy, Services: []*messages.ServiceStatus{service("web", healthy)}}, models.Healthy},
		{"unhealthy without services from older agents", &messages.ApplicationStatus{Health: unhealthy}, models.Unhealthy},
		{"all services unhealthy", &messages.ApplicationStatus{Health: unhealthy, Services: []*messages.ServiceStatus{service("db", unhealthy), service("web", stopped)}}, models.Unhealthy},
		{"one healthy and one unhealthy service", &messages.ApplicationStatus{Health: unhealthy, Services: []*messages.ServiceStatus{service("db", healthy), service("web", unhealthy)}}, models.Degraded},
		{"one healthy and one stopped service", &messages.ApplicationStatus{Health: unhealthy, Services: []*messages.ServiceStatus{service("db", healthy), service("web", stopped)}}, models.Degraded},
		{"stopped", &messages.ApplicationStatus{Health: stopped, Services: []*messages.ServiceStatus{service("web", stopped)}}, models.Stopped},
		{"unknown", &messages.ApplicationStatus{Health: messages.HealthStatus_HEALTH_STATUS_UNSPECIFIED}, models.UnknownHealth},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reportedApplicationHealth(tt.status); got != tt.want {
				t.Errorf("reportedApplicationHealth() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHandleApplicationStatusReport_RecordsHealthHistory(t *testing.T) {
	setupDeployTestEnv(t)
	app := seedAppForAgent(t, "agent-1")
	other := seedAppForAgent(t, "agent-2")

	nop := zerolog.Nop()
	client := &Client{Id: "agent-1"}
	report := func(appID string, health messages.HealthStatus, services ...*messages.ServiceStatus) {
		handleApplicationStatusReport(t.Context(), client, &messages.ApplicationStatusReport{
			Statuses: []*messages.ApplicationStatus{{ApplicationId: appID, Health: health, Services: services}},
		}, &nop)
	}

	report(app.Id, messages.HealthStatus_HEALTH_STATUS_HEALTHY)
	report(app.Id, messages.HealthStatus_HEALTH_STATUS_HEALTHY)
	report(app.Id, messages.HealthStatus_HEALTH_STATUS_UNHEALTHY,
		&messages.ServiceStatus{Name: "db", Health: messages.HealthStatus_HEALTH_STATUS_HEALTHY},
		&messages.ServiceStatus{Name: "web", Health: messages.HealthStatus_HEALTH_STATUS_UNHEALTHY},
		nil,
	)
	// Spoofed: belongs to a different agent — must not be recorded.
	report(other.Id, messages.HealthStatus_HEALTH_STATUS_HEALTHY)

	changes, err := gorm.G[models.ApplicationHealthChange](db.DB).Order("changed_at, id").Find(t.Context())
	if err != nil {
		t.Fatalf("load health changes: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("expected 2 health changes, got %d: %+v", len(changes), changes)
	}
	if changes[0].ApplicationId != app.Id || changes[0].Status != models.Healthy {
		t.Errorf("expected healthy change first, got %+v", changes[0])
	}
	degraded := changes[1]
	if degraded.Status != models.Degraded || len(degraded.Services) != 1 ||
		degraded.Services[0] != (models.ServiceHealth{Name: "web", Status: models.Unhealthy}) {
		t.Errorf("expected degraded change with failing service web, got %+v", degraded)
	}
}

func TestHandleApplicationStatusReport_SyncsHealthHistoryOncePerConnection(t *testing.T) {
	setupDeployTestEnv(t)
	app := seedAppForAgent(t, "agent-1")

	nop := zerolog.Nop()
	report := func(client *Client) {
		handleApplicationStatusReport(t.Context(), client, &messages.ApplicationStatusReport{
			Statuses: []*messages.ApplicationStatus{nil, {ApplicationId: app.Id, Health: messages.HealthStatus_HEALTH_STATUS_HEALTHY}},
		}, &nop)
	}
	latest := func() models.HealthStatus {
		t.Helper()
		change, err := healthhistory.Latest(t.Context(), app.Id)
		if err != nil {
			t.Fatalf("Latest() error: %v", err)
		}
		return change.Status
	}

	client := &Client{Id: "agent-1"}
	report(client)
	if got := latest(); got != models.Healthy {
		t.Fatalf("expected healthy to be recorded, got %q", got)
	}

	// A hub restart records the agent offline but keeps the stored health.
	if err := healthhistory.RecordAgentOffline(t.Context(), "agent-1", time.Now()); err != nil {
		t.Fatalf("RecordAgentOffline() error: %v", err)
	}
	report(client)
	if got := latest(); got != models.UnknownHealth {
		t.Errorf("an unchanged report must not be recorded again on the same connection, got %q", got)
	}

	report(&Client{Id: "agent-1"})
	if got := latest(); got != models.Healthy {
		t.Errorf("expected the first report of a new connection to sync the history, got %q", got)
	}
	if got := healthOf(t, app.Id); got != models.Healthy {
		t.Errorf("expected the stored health to stay healthy, got %q", got)
	}
}
