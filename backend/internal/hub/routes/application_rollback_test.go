package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	application_deployer "github.com/OrcaCD/orca-cd/internal/hub/deployer"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func seedRouteDeployment(t *testing.T, appID string, status models.ApplicationDeploymentStatus) models.ApplicationDeployment {
	t.Helper()
	now := time.Now()
	deployment := models.ApplicationDeployment{
		ApplicationId: appID,
		Status:        status,
		Commit:        "0123456789",
		CommitMessage: "known good",
		ComposeFile:   crypto.EncryptedString("services: {}\n"),
		Images:        []models.DeployedImage{{Service: "web", ImageId: "sha256:web"}},
		DeployedAt:    &now,
	}
	if err := gorm.G[models.ApplicationDeployment](db.DB).Create(t.Context(), &deployment); err != nil {
		t.Fatalf("seed deployment: %v", err)
	}
	return deployment
}

func invokeRollbackHandler(t *testing.T, appID, deploymentID string) *httptest.ResponseRecorder {
	t.Helper()
	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/applications/"+appID+"/deployments/"+deploymentID+"/rollback", nil)
	c.Params = gin.Params{{Key: "id", Value: appID}, {Key: "deploymentId", Value: deploymentID}}
	RollbackApplicationHandler(c)
	return w
}

func useStubRouteDeployer(t *testing.T, deployer *stubRouteDeployer) {
	t.Helper()
	application_deployer.DefaultApplicationDeployer = deployer
	t.Cleanup(func() { application_deployer.DefaultApplicationDeployer = nil })
}

func TestRollbackApplicationHandler_Success(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-rollback")
	agent := seedTestAgent(t, "agent-rollback")
	app := seedTestApplication(t, repo.Id, agent.Id, "Roll Me Back")
	target := seedRouteDeployment(t, app.Id, models.DeploymentSucceeded)
	deployer := &stubRouteDeployer{}
	useStubRouteDeployer(t, deployer)

	w := invokeRollbackHandler(t, app.Id, target.Id)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	if deployer.rolledBack == nil || deployer.rolledBack.Id != target.Id {
		t.Fatalf("expected a rollback to %q, got %+v", target.Id, deployer.rolledBack)
	}
	event, err := gorm.G[models.ApplicationEvent](db.DB).Where("request_id = ?", deployer.requestID).First(t.Context())
	if err != nil {
		t.Fatalf("load rollback event: %v", err)
	}
	if event.Type != models.ApplicationEventRollback ||
		event.Source != models.ApplicationEventSourceManual ||
		event.Status != models.ApplicationEventRunning ||
		event.DeploymentId == nil || *event.DeploymentId != target.Id ||
		event.CommitHash == nil || *event.CommitHash != target.Commit ||
		event.ActorName == nil || *event.ActorName != "Test User" {
		t.Fatalf("unexpected rollback event: %+v", event)
	}
}

func TestRollbackApplicationHandler_Rejects(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T, app models.Application) string
		wantCode int
	}{
		{
			name:     "unknown deployment",
			setup:    func(*testing.T, models.Application) string { return "missing" },
			wantCode: http.StatusNotFound,
		},
		{
			name: "pending deployment",
			setup: func(t *testing.T, app models.Application) string {
				return seedRouteDeployment(t, app.Id, models.DeploymentPending).Id
			},
			wantCode: http.StatusNotFound,
		},
		{
			name: "current deployment",
			setup: func(t *testing.T, app models.Application) string {
				target := seedRouteDeployment(t, app.Id, models.DeploymentSucceeded)
				if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).
					Update(t.Context(), "current_deployment_id", target.Id); err != nil {
					t.Fatalf("set current deployment: %v", err)
				}
				return target.Id
			},
			wantCode: http.StatusConflict,
		},
		{
			name: "deployment in progress",
			setup: func(t *testing.T, app models.Application) string {
				if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).
					Update(t.Context(), "sync_status", models.Syncing); err != nil {
					t.Fatalf("set syncing: %v", err)
				}
				return seedRouteDeployment(t, app.Id, models.DeploymentSucceeded).Id
			},
			wantCode: http.StatusConflict,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupTestDBWithApplications(t)
			repo := seedTestRepository(t, "https://github.com/owner/repo-rollback-reject")
			agent := seedTestAgent(t, "agent-rollback-reject")
			app := seedTestApplication(t, repo.Id, agent.Id, "Roll Me Back")
			deployer := &stubRouteDeployer{}
			useStubRouteDeployer(t, deployer)

			w := invokeRollbackHandler(t, app.Id, tt.setup(t, app))

			if w.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d: %s", tt.wantCode, w.Code, w.Body.String())
			}
			if deployer.rolledBack != nil {
				t.Fatal("expected no rollback to be dispatched")
			}
		})
	}
}

func TestRollbackApplicationHandler_OtherApplicationsDeployment(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-rollback-other")
	agent := seedTestAgent(t, "agent-rollback-other")
	app := seedTestApplication(t, repo.Id, agent.Id, "Roll Me Back")
	other := seedTestApplication(t, repo.Id, agent.Id, "Other App")
	target := seedRouteDeployment(t, other.Id, models.DeploymentSucceeded)
	useStubRouteDeployer(t, &stubRouteDeployer{})

	if w := invokeRollbackHandler(t, app.Id, target.Id); w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRollbackApplicationHandler_AgentOffline(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-rollback-offline")
	agent := seedTestAgent(t, "agent-rollback-offline")
	app := seedTestApplication(t, repo.Id, agent.Id, "Roll Me Back")
	target := seedRouteDeployment(t, app.Id, models.DeploymentSucceeded)
	useStubRouteDeployer(t, &stubRouteDeployer{startErr: application_deployer.ErrAgentOffline})

	if w := invokeRollbackHandler(t, app.Id, target.Id); w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestResumeApplicationAutoSyncHandler(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-resume")
	agent := seedTestAgent(t, "agent-resume")
	app := seedTestApplication(t, repo.Id, agent.Id, "Resume Me")
	now := time.Now()
	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).
		Select("RolledBackAt", "AutoSyncPaused").
		Updates(t.Context(), models.Application{RolledBackAt: &now, AutoSyncPaused: true}); err != nil {
		t.Fatalf("pause auto-sync: %v", err)
	}

	for _, id := range []string{app.Id, app.Id, "missing"} {
		c, w := makeAuthContext(t, "user-1")
		c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/applications/"+id+"/resume-auto-sync", nil)
		c.Params = gin.Params{{Key: "id", Value: id}}
		ResumeApplicationAutoSyncHandler(c)

		want := http.StatusOK
		if id == "missing" {
			want = http.StatusNotFound
		}
		if w.Code != want {
			t.Fatalf("%s: expected %d, got %d: %s", id, want, w.Code, w.Body.String())
		}
	}

	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(t.Context())
	if err != nil {
		t.Fatalf("load application: %v", err)
	}
	if updated.AutoSyncPaused {
		t.Error("expected auto-sync to be resumed")
	}
	if updated.RolledBackAt == nil {
		t.Error("resuming auto-sync must keep the application rolled back")
	}
}

func TestPullApplicationImagesHandler_RolledBack(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-pull-rolled-back")
	agent := seedTestAgent(t, "agent-pull-rolled-back")
	app := seedTestApplication(t, repo.Id, agent.Id, "Pull Me")
	now := time.Now()
	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).
		Update(t.Context(), "rolled_back_at", now); err != nil {
		t.Fatalf("mark rolled back: %v", err)
	}

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/applications/"+app.Id+"/pull-images", nil)
	c.Params = gin.Params{{Key: "id", Value: app.Id}}
	PullApplicationImagesHandler(c)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func invokeRollbackSettingsHandler(t *testing.T, appID, body string) *httptest.ResponseRecorder {
	t.Helper()
	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/applications/"+appID+"/rollback", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: appID}}
	UpdateRollbackSettingsHandler(c)
	return w
}

func TestUpdateRollbackSettingsHandler(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-rollback-settings")
	agent := seedTestAgent(t, "agent-rollback-settings")
	app := seedTestApplication(t, repo.Id, agent.Id, "Settings App")

	w := invokeRollbackSettingsHandler(t, app.Id, `{"enabled":true,"onDeployFailure":false,"onUnhealthy":true,"healthTimeoutSeconds":120,"pauseAutoSync":false}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response applicationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	want := rollbackSettingsResponse{Enabled: true, OnUnhealthy: true, HealthTimeoutSeconds: 120}
	if response.Rollback != want {
		t.Errorf("rollback settings = %+v, want %+v", response.Rollback, want)
	}
	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(t.Context())
	if err != nil {
		t.Fatalf("load application: %v", err)
	}
	if !updated.RollbackEnabled || updated.RollbackOnDeployFailure || !updated.RollbackOnUnhealthy ||
		updated.RollbackHealthTimeoutSeconds != 120 || updated.RollbackPauseAutoSync {
		t.Errorf("unexpected stored settings: %+v", updated)
	}
}

func TestUpdateRollbackSettingsHandler_Rejects(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-rollback-settings-invalid")
	agent := seedTestAgent(t, "agent-rollback-settings-invalid")
	app := seedTestApplication(t, repo.Id, agent.Id, "Settings App")

	for _, tt := range []struct {
		id, body string
		want     int
	}{
		{app.Id, `{"enabled":true,"healthTimeoutSeconds":10}`, http.StatusBadRequest},
		{app.Id, `{"enabled":true,"healthTimeoutSeconds":3601}`, http.StatusBadRequest},
		{app.Id, `not json`, http.StatusBadRequest},
		{"missing", `{"enabled":true,"healthTimeoutSeconds":300}`, http.StatusNotFound},
	} {
		if w := invokeRollbackSettingsHandler(t, tt.id, tt.body); w.Code != tt.want {
			t.Errorf("%s %s: expected %d, got %d: %s", tt.id, tt.body, tt.want, w.Code, w.Body.String())
		}
	}
}
