package mcpserver

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	application_deployer "github.com/OrcaCD/orca-cd/internal/hub/deployer"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"
)

const runningComposeFile = "services:\n  web:\n    image: nginx:1.25\n"

func useStubDeployer(t *testing.T) *stubDeployer {
	t.Helper()
	deployer := &stubDeployer{}
	application_deployer.DefaultApplicationDeployer = deployer
	t.Cleanup(func() { application_deployer.DefaultApplicationDeployer = nil })
	return deployer
}

// seedRunningDeployment records that the agent runs an older compose file.
func seedRunningDeployment(t *testing.T, app *models.Application) {
	t.Helper()
	deployment := models.ApplicationDeployment{
		ApplicationId: app.Id,
		Status:        models.DeploymentSucceeded,
		Commit:        "old-commit",
		ComposeFile:   crypto.EncryptedString(runningComposeFile),
		Images:        []models.DeployedImage{},
	}
	if err := db.DB.Create(&deployment).Error; err != nil {
		t.Fatalf("failed to seed deployment: %v", err)
	}
	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).Update(t.Context(), "current_deployment_id", deployment.Id); err != nil {
		t.Fatalf("failed to set current deployment: %v", err)
	}
}

func TestPreviewDeploy_ShowsDiffToRunningDeployment(t *testing.T) {
	setupTestDB(t)
	app := seedApplication(t, "web")
	seedRunningDeployment(t, &app)

	session := connect(t, testUser(), rbac.Admin())

	var out previewDeployOutput
	if errText := callTool(t, session, "preview_deploy", map[string]any{"applicationId": app.Id}, &out); errText != "" {
		t.Fatalf("preview_deploy failed: %s", errText)
	}
	if !out.Changed || out.RunningCommit != "old-commit" || out.Commit != "new-commit" {
		t.Errorf("unexpected preview: %+v", out)
	}
	if !strings.Contains(out.Diff, "-    image: nginx:1.25") || !strings.Contains(out.Diff, "+    image: nginx:1.27") {
		t.Errorf("diff doesn't show the image change:\n%s", out.Diff)
	}
	if out.PreviewId == "" {
		t.Error("expected a preview id")
	}
}

func TestPreviewDeploy_WarnsAboutOfflineAgentAndUnknownDeployment(t *testing.T) {
	setupTestDB(t)
	app := seedApplication(t, "web")
	if _, err := gorm.G[models.Agent](db.DB).Where("id = ?", app.AgentId).Update(t.Context(), "status", models.AgentStatusOffline); err != nil {
		t.Fatalf("failed to mark agent offline: %v", err)
	}

	session := connect(t, testUser(), rbac.Admin())

	var out previewDeployOutput
	if errText := callTool(t, session, "preview_deploy", map[string]any{"applicationId": app.Id}, &out); errText != "" {
		t.Fatalf("preview_deploy failed: %s", errText)
	}
	if !strings.Contains(out.Diff, "+    image: nginx:1.27") {
		t.Errorf("expected the whole compose file as added, got:\n%s", out.Diff)
	}
	if len(out.Warnings) != 2 {
		t.Errorf("expected warnings about the unknown deployment and the offline agent, got %v", out.Warnings)
	}
}

func TestDeployApplication_DeploysPreview(t *testing.T) {
	setupTestDB(t)
	deployer := useStubDeployer(t)
	app := seedApplication(t, "web")
	seedRunningDeployment(t, &app)

	session := connect(t, testUser(), rbac.Admin())

	var previewed previewDeployOutput
	if errText := callTool(t, session, "preview_deploy", map[string]any{"applicationId": app.Id}, &previewed); errText != "" {
		t.Fatalf("preview_deploy failed: %s", errText)
	}

	var out deployApplicationOutput
	if errText := callTool(t, session, "deploy_application", map[string]any{"applicationId": app.Id, "previewId": previewed.PreviewId}, &out); errText != "" {
		t.Fatalf("deploy_application failed: %s", errText)
	}
	if len(deployer.deployed) != 1 || deployer.deployed[0] != app.Id {
		t.Fatalf("expected %q to be deployed, got %v", app.Id, deployer.deployed)
	}

	// A preview is deployed once, a retry needs a new one.
	if errText := callTool(t, session, "deploy_application", map[string]any{"applicationId": app.Id, "previewId": previewed.PreviewId}, nil); errText != errPreviewMismatch.Error() {
		t.Errorf("second deploy error = %q, want %q", errText, errPreviewMismatch.Error())
	}
	// Nor with another spelling of the same signature: the last character
	// has bits a lenient decoder ignores.
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	last := strings.IndexByte(alphabet, previewed.PreviewId[len(previewed.PreviewId)-1])
	respelled := previewed.PreviewId[:len(previewed.PreviewId)-1] + string(alphabet[last^1])
	if errText := callTool(t, session, "deploy_application", map[string]any{"applicationId": app.Id, "previewId": respelled}, nil); errText != errPreviewMismatch.Error() {
		t.Errorf("respelled deploy error = %q, want %q", errText, errPreviewMismatch.Error())
	}
	if len(deployer.deployed) != 1 {
		t.Errorf("expected one deploy, got %v", deployer.deployed)
	}

	event, err := gorm.G[models.ApplicationEvent](db.DB).Where("application_id = ?", app.Id).First(t.Context())
	if err != nil {
		t.Fatalf("load deployment event: %v", err)
	}
	if event.Type != models.ApplicationEventDeployment || event.ActorName == nil || *event.ActorName != "Test User" {
		t.Errorf("unexpected deployment event: %+v", event)
	}
}

func TestDeployApplication_RefusesStalePreviews(t *testing.T) {
	tests := []struct {
		name   string
		change func(t *testing.T, app *models.Application)
		// otherUser deploys the preview with another user.
		otherUser bool
	}{
		{
			name: "compose file changed",
			change: func(t *testing.T, app *models.Application) {
				if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).Update(t.Context(), "compose_file", crypto.EncryptedString("services: {}\n")); err != nil {
					t.Fatalf("failed to change compose file: %v", err)
				}
			},
		},
		{
			name: "running deployment changed",
			change: func(t *testing.T, app *models.Application) {
				seedRunningDeployment(t, app)
			},
		},
		{
			name: "expired",
			change: func(t *testing.T, _ *models.Application) {
				later := time.Now().Add(previewTTL + time.Minute)
				now = func() time.Time { return later }
				t.Cleanup(func() { now = time.Now })
			},
		},
		{
			name: "commit changed",
			change: func(t *testing.T, app *models.Application) {
				if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).Update(t.Context(), "commit", "other-commit"); err != nil {
					t.Fatalf("failed to change commit: %v", err)
				}
			},
		},
		{
			name: "rolled back",
			change: func(t *testing.T, app *models.Application) {
				if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).Update(t.Context(), "rolled_back_at", time.Now()); err != nil {
					t.Fatalf("failed to roll back: %v", err)
				}
			},
		},
		{
			name: "renamed",
			change: func(t *testing.T, app *models.Application) {
				if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).Update(t.Context(), "name", crypto.EncryptedString("renamed")); err != nil {
					t.Fatalf("failed to rename: %v", err)
				}
			},
		},
		{name: "other user", otherUser: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupTestDB(t)
			deployer := useStubDeployer(t)
			app := seedApplication(t, "web")

			session := connect(t, testUser(), rbac.Admin())
			var previewed previewDeployOutput
			if errText := callTool(t, session, "preview_deploy", map[string]any{"applicationId": app.Id}, &previewed); errText != "" {
				t.Fatalf("preview_deploy failed: %s", errText)
			}

			if tt.change != nil {
				tt.change(t, &app)
			}
			if tt.otherUser {
				session = connect(t, &models.User{Id: "user-2", Name: "Other"}, rbac.Admin())
			}

			errText := callTool(t, session, "deploy_application", map[string]any{"applicationId": app.Id, "previewId": previewed.PreviewId}, nil)
			if errText != errPreviewMismatch.Error() {
				t.Errorf("error = %q, want %q", errText, errPreviewMismatch.Error())
			}
			if len(deployer.deployed) != 0 {
				t.Errorf("expected no deploy, got %v", deployer.deployed)
			}
		})
	}
}

func TestDeployApplication_RequiresDeployPermission(t *testing.T) {
	setupTestDB(t)
	deployer := useStubDeployer(t)
	allowed := seedApplication(t, "allowed")
	readOnly := seedApplication(t, "read-only")

	session := connect(t, testUser(), rbac.New([]models.RolePermission{
		grant(models.ResourceApplication, models.ActionDeploy, allowed.Id),
		grant(models.ResourceApplication, models.ActionRead, readOnly.Id),
	}))

	calls := map[string]map[string]any{
		"preview_deploy":     {"applicationId": readOnly.Id},
		"deploy_application": {"applicationId": readOnly.Id, "previewId": "0.invalid"},
	}
	for tool, args := range calls {
		if errText := callTool(t, session, tool, args, nil); errText != errApplicationNotFound.Error() {
			t.Errorf("%s error = %q, want %q", tool, errText, errApplicationNotFound.Error())
		}
	}
	if len(deployer.deployed) != 0 {
		t.Errorf("expected no deploy, got %v", deployer.deployed)
	}
}

func TestDeployApplication_AgentOffline(t *testing.T) {
	setupTestDB(t)
	deployer := useStubDeployer(t)
	deployer.err = application_deployer.ErrAgentOffline
	app := seedApplication(t, "web")

	session := connect(t, testUser(), rbac.Admin())
	var previewed previewDeployOutput
	if errText := callTool(t, session, "preview_deploy", map[string]any{"applicationId": app.Id}, &previewed); errText != "" {
		t.Fatalf("preview_deploy failed: %s", errText)
	}

	errText := callTool(t, session, "deploy_application", map[string]any{"applicationId": app.Id, "previewId": previewed.PreviewId}, nil)
	if !strings.Contains(errText, "agent is offline") {
		t.Errorf("error = %q, want an offline agent error", errText)
	}
}

func TestDeployApplication_ConcurrentCallsDeployOnce(t *testing.T) {
	setupTestDB(t)
	deployer := useStubDeployer(t)
	app := seedApplication(t, "web")

	session := connect(t, testUser(), rbac.Admin())
	var previewed previewDeployOutput
	if errText := callTool(t, session, "preview_deploy", map[string]any{"applicationId": app.Id}, &previewed); errText != "" {
		t.Fatalf("preview_deploy failed: %s", errText)
	}

	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			_, _ = session.CallTool(t.Context(), &mcp.CallToolParams{
				Name:      "deploy_application",
				Arguments: map[string]any{"applicationId": app.Id, "previewId": previewed.PreviewId},
			})
		})
	}
	wg.Wait()

	if len(deployer.deployed) != 1 {
		t.Errorf("expected one deploy, got %v", deployer.deployed)
	}
}
