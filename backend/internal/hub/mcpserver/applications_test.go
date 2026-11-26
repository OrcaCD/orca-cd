package mcpserver

import (
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
)

func TestListApplications_OnlyReadable(t *testing.T) {
	setupTestDB(t)
	visible := seedApplication(t, "visible")
	seedApplication(t, "hidden")

	session := connect(t, testUser(), rbac.New([]models.RolePermission{grant(models.ResourceApplication, models.ActionRead, visible.Id)}))

	var out listApplicationsOutput
	if errText := callTool(t, session, "list_applications", map[string]any{}, &out); errText != "" {
		t.Fatalf("list_applications failed: %s", errText)
	}
	if len(out.Applications) != 1 || out.Applications[0].Id != visible.Id {
		t.Fatalf("expected only %q, got %+v", visible.Id, out.Applications)
	}
	got := out.Applications[0]
	if got.Name != "visible" || got.AgentName != "agent-visible" || got.RepositoryName != "owner/visible" || got.SyncStatus != string(models.OutOfSync) {
		t.Errorf("unexpected summary: %+v", got)
	}
}

func TestGetApplication_ReturnsComposeAndRecentErrors(t *testing.T) {
	setupTestDB(t)
	app := seedApplication(t, "web")

	failed := "pull access denied for nginx"
	completed := time.Now()
	event := models.ApplicationEvent{
		ApplicationId: app.Id,
		Type:          models.ApplicationEventDeployment,
		Source:        models.ApplicationEventSourceManual,
		Status:        models.ApplicationEventFailed,
		ErrorMessage:  &failed,
		CompletedAt:   &completed,
	}
	if err := db.DB.Create(&event).Error; err != nil {
		t.Fatalf("failed to seed event: %v", err)
	}

	session := connect(t, testUser(), rbac.Admin())

	var out applicationDetails
	if errText := callTool(t, session, "get_application", map[string]any{"applicationId": app.Id}, &out); errText != "" {
		t.Fatalf("get_application failed: %s", errText)
	}
	if out.ComposeFile != testComposeFile {
		t.Errorf("composeFile = %q, want %q", out.ComposeFile, testComposeFile)
	}
	if len(out.RecentEvents) != 1 || out.RecentEvents[0].Error != failed || out.RecentEvents[0].Status != string(models.ApplicationEventFailed) {
		t.Errorf("unexpected events: %+v", out.RecentEvents)
	}
}

func TestGetApplication_HidesApplicationsWithoutPermission(t *testing.T) {
	setupTestDB(t)
	allowed := seedApplication(t, "allowed")
	other := seedApplication(t, "other")

	session := connect(t, testUser(), rbac.New([]models.RolePermission{grant(models.ResourceApplication, models.ActionRead, allowed.Id)}))

	for _, id := range []string{other.Id, "does-not-exist"} {
		if errText := callTool(t, session, "get_application", map[string]any{"applicationId": id}, nil); errText != errApplicationNotFound.Error() {
			t.Errorf("get_application(%q) error = %q, want %q", id, errText, errApplicationNotFound.Error())
		}
	}
}

func TestListAgents(t *testing.T) {
	setupTestDB(t)
	app := seedApplication(t, "web")

	session := connect(t, testUser(), rbac.Admin())

	var out listAgentsOutput
	if errText := callTool(t, session, "list_agents", map[string]any{}, &out); errText != "" {
		t.Fatalf("list_agents failed: %s", errText)
	}
	if len(out.Agents) != 1 || out.Agents[0].Id != app.AgentId || out.Agents[0].Status != "online" {
		t.Errorf("unexpected agents: %+v", out.Agents)
	}
}
