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
