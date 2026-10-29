package websocket

import (
	"context"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/applicationevents"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	application_deployer "github.com/OrcaCD/orca-cd/internal/hub/deployer"
	"github.com/OrcaCD/orca-cd/internal/hub/deployments"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// rollbackRecorder records the rollbacks the hub dispatches instead of
// sending them to an agent.
type rollbackRecorder struct {
	targets []string
}

func (r *rollbackRecorder) TriggerApplicationDeploy(context.Context, *models.Application, string, string) error {
	return nil
}

func (r *rollbackRecorder) TriggerRollback(_ context.Context, _ *models.Application, target *models.ApplicationDeployment, _ string) error {
	r.targets = append(r.targets, target.Id)
	return nil
}

func recordRollbacks(t *testing.T) *rollbackRecorder {
	t.Helper()
	recorder := &rollbackRecorder{}
	previous := application_deployer.DefaultApplicationDeployer
	application_deployer.DefaultApplicationDeployer = recorder
	t.Cleanup(func() { application_deployer.DefaultApplicationDeployer = previous })
	return recorder
}

// registerAgent makes the agent count as connected for the health gate.
func registerAgent(t *testing.T, agentID string) {
	t.Helper()
	log := testLogger()
	previous := DefaultHub
	DefaultHub = NewHub(&log)
	t.Cleanup(func() { DefaultHub = previous })
	conn := newTestWSConn(t)
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := DefaultHub.Register(agentID, conn); err != nil {
		t.Fatalf("Register: %v", err)
	}
}

func enableAutomaticRollback(t *testing.T, appID string) models.Application {
	t.Helper()
	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", appID).
		Select("RollbackEnabled", "RollbackOnDeployFailure", "RollbackOnUnhealthy", "RollbackHealthTimeoutSeconds", "RollbackPauseAutoSync").
		Updates(t.Context(), models.Application{
			RollbackEnabled:              true,
			RollbackOnDeployFailure:      true,
			RollbackOnUnhealthy:          true,
			RollbackHealthTimeoutSeconds: 300,
			RollbackPauseAutoSync:        false,
		}); err != nil {
		t.Fatalf("enable automatic rollback: %v", err)
	}
	return loadDeployTestApp(t, appID)
}

func startDeployEvent(t *testing.T, app *models.Application, requestID string) {
	t.Helper()
	if _, err := applicationevents.Start(t.Context(), applicationevents.Params{
		ApplicationID: app.Id,
		RequestID:     &requestID,
		Type:          models.ApplicationEventDeployment,
		Source:        models.ApplicationEventSourceManual,
	}); err != nil {
		t.Fatalf("start event: %v", err)
	}
	if _, err := deployments.Start(t.Context(), app, "services: {}\n", requestID); err != nil {
		t.Fatalf("start deployment: %v", err)
	}
}

// deployGated deploys the application under a health gate after a succeeded
// deployment it can be rolled back to, which it returns.
func deployGated(t *testing.T, app *models.Application) models.ApplicationDeployment {
	t.Helper()
	nop := zerolog.Nop()
	good := seedRollbackTarget(t, app.Id)
	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).
		Update(t.Context(), "current_deployment_id", good.Id); err != nil {
		t.Fatalf("set current deployment: %v", err)
	}
	startDeployEvent(t, app, "gated")
	handleDeployResult(t.Context(), "agent-1", &messages.DeployResult{RequestId: "gated", ApplicationId: app.Id, Success: true}, &nop)
	return good
}

func verifyingDeployment(t *testing.T, appID string) *models.ApplicationDeployment {
	t.Helper()
	deployment, err := deployments.Verifying(t.Context(), appID)
	if err != nil {
		t.Fatalf("load verifying deployment: %v", err)
	}
	return deployment
}

func TestHandleDeployResult_HealthGateKeepsEventRunning(t *testing.T) {
	setupDeployTestEnv(t)
	app := enableAutomaticRollback(t, seedDeployApp(t, models.Syncing).Id)

	deployGated(t, &app)

	if event := loadEventByRequest(t, "gated"); event.Status != models.ApplicationEventRunning {
		t.Errorf("event status = %q, want running until the health check passed", event.Status)
	}
	deployment := verifyingDeployment(t, app.Id)
	if deployment == nil {
		t.Fatal("expected the deployment to be verified")
	}
	if got := deployment.VerifyUntil.Sub(*deployment.DeployedAt); got != 5*time.Minute {
		t.Errorf("health gate = %s, want 5m", got)
	}
	if updated := loadDeployTestApp(t, app.Id); updated.SyncStatus != models.Synced {
		t.Errorf("sync status = %q, want synced", updated.SyncStatus)
	}
}

func TestHandleDeployResult_NoHealthGateWithoutAutomaticRollback(t *testing.T) {
	setupDeployTestEnv(t)
	app := seedDeployApp(t, models.Syncing)
	nop := zerolog.Nop()
	expectAsyncNotification(t)
	startDeployEvent(t, &app, "ungated")

	handleDeployResult(t.Context(), "agent-1", &messages.DeployResult{RequestId: "ungated", ApplicationId: app.Id, Success: true}, &nop)

	if event := loadEventByRequest(t, "ungated"); event.Status != models.ApplicationEventSucceeded {
		t.Errorf("event status = %q, want succeeded", event.Status)
	}
	if deployment := verifyingDeployment(t, app.Id); deployment != nil {
		t.Errorf("expected no health check, got %+v", deployment)
	}
}

func TestCheckHealthGate_UnhealthyRollsBack(t *testing.T) {
	setupDeployTestEnv(t)
	app := enableAutomaticRollback(t, seedDeployApp(t, models.Syncing).Id)
	good := deployGated(t, &app)
	recorder := recordRollbacks(t)
	nop := zerolog.Nop()
	expectAsyncNotification(t)

	checkHealthGate(t.Context(), "agent-1", app.Id, models.Healthy, &nop)
	if len(recorder.targets) != 0 || verifyingDeployment(t, app.Id) == nil {
		t.Fatal("a healthy report must not end the health check")
	}
	checkHealthGate(t.Context(), "other-agent", app.Id, models.Unhealthy, &nop)
	if len(recorder.targets) != 0 {
		t.Fatal("a report of another agent must be ignored")
	}

	checkHealthGate(t.Context(), "agent-1", app.Id, models.Unhealthy, &nop)

	if len(recorder.targets) != 1 || recorder.targets[0] != good.Id {
		t.Fatalf("expected a rollback to %q, got %v", good.Id, recorder.targets)
	}
	event := loadEventByRequest(t, "gated")
	if event.Status != models.ApplicationEventFailed || event.ErrorMessage == nil {
		t.Fatalf("unexpected deployment event: %+v", event)
	}
	if want := "health check failed: application was unhealthy 0s after the deployment"; *event.ErrorMessage != want {
		t.Errorf("error = %q, want %q", *event.ErrorMessage, want)
	}
	if verifyingDeployment(t, app.Id) != nil {
		t.Error("expected the unhealthy deployment to be dropped")
	}
	rollbackEvent, err := gorm.G[models.ApplicationEvent](db.DB).
		Where("application_id = ? AND type = ?", app.Id, models.ApplicationEventRollback).
		First(t.Context())
	if err != nil {
		t.Fatalf("load rollback event: %v", err)
	}
	if rollbackEvent.Source != models.ApplicationEventSourceHealthCheck {
		t.Errorf("rollback source = %q, want health_check", rollbackEvent.Source)
	}
	updated := loadDeployTestApp(t, app.Id)
	if updated.LastSyncError == nil || *updated.LastSyncError != *event.ErrorMessage {
		t.Errorf("last sync error = %v, want the health check failure", updated.LastSyncError)
	}
}

func TestResolveEndedHealthGates(t *testing.T) {
	tests := []struct {
		name         string
		health       models.HealthStatus
		connected    bool
		wantEvent    models.ApplicationEventStatus
		wantRollback bool
	}{
		{name: "healthy passes", health: models.Healthy, connected: true, wantEvent: models.ApplicationEventSucceeded},
		{name: "not healthy fails", health: models.UnknownHealth, connected: true, wantEvent: models.ApplicationEventFailed, wantRollback: true},
		{name: "offline agent waits", health: models.UnknownHealth, wantEvent: models.ApplicationEventRunning},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupDeployTestEnv(t)
			app := enableAutomaticRollback(t, seedDeployApp(t, models.Syncing).Id)
			good := deployGated(t, &app)
			recorder := recordRollbacks(t)
			if tt.connected {
				registerAgent(t, "agent-1")
			}
			if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).
				Update(t.Context(), "health_status", tt.health); err != nil {
				t.Fatalf("set health: %v", err)
			}
			nop := zerolog.Nop()
			if tt.wantEvent != models.ApplicationEventRunning {
				expectAsyncNotification(t)
			}

			resolveEndedHealthGates(t.Context(), time.Now(), &nop)
			if event := loadEventByRequest(t, "gated"); event.Status != models.ApplicationEventRunning {
				t.Fatalf("a health check must not end before its timeout, got %q", event.Status)
			}
			resolveEndedHealthGates(t.Context(), time.Now().Add(6*time.Minute), &nop)

			if event := loadEventByRequest(t, "gated"); event.Status != tt.wantEvent {
				t.Errorf("event status = %q, want %q", event.Status, tt.wantEvent)
			}
			if got := len(recorder.targets) == 1 && recorder.targets[0] == good.Id; got != tt.wantRollback {
				t.Errorf("rolled back = %v, want %v", recorder.targets, tt.wantRollback)
			}
			if tt.wantEvent == models.ApplicationEventFailed {
				event := loadEventByRequest(t, "gated")
				if want := "health check failed: application did not become healthy within 300s"; event.ErrorMessage == nil || *event.ErrorMessage != want {
					t.Errorf("error = %v, want %q", event.ErrorMessage, want)
				}
			}
		})
	}
}

func TestHandleDeployResult_FailureRollsBack(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "enabled", false: "disabled"}[enabled], func(t *testing.T) {
			setupDeployTestEnv(t)
			app := seedDeployApp(t, models.Syncing)
			if enabled {
				app = enableAutomaticRollback(t, app.Id)
			}
			good := seedRollbackTarget(t, app.Id)
			if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).
				Update(t.Context(), "current_deployment_id", good.Id); err != nil {
				t.Fatalf("set current deployment: %v", err)
			}
			startDeployEvent(t, &app, "failing")
			recorder := recordRollbacks(t)
			nop := zerolog.Nop()
			expectAsyncNotification(t)

			handleDeployResult(t.Context(), "agent-1", &messages.DeployResult{RequestId: "failing", ApplicationId: app.Id, ErrorMessage: "compose up failed"}, &nop)

			if !enabled {
				if len(recorder.targets) != 0 {
					t.Fatalf("expected no rollback, got %v", recorder.targets)
				}
				return
			}
			if len(recorder.targets) != 1 || recorder.targets[0] != good.Id {
				t.Fatalf("expected a rollback to %q, got %v", good.Id, recorder.targets)
			}
			rollbackEvent, err := gorm.G[models.ApplicationEvent](db.DB).
				Where("application_id = ? AND type = ?", app.Id, models.ApplicationEventRollback).
				First(t.Context())
			if err != nil {
				t.Fatalf("load rollback event: %v", err)
			}
			if rollbackEvent.Source != models.ApplicationEventSourceDeploymentFailure {
				t.Errorf("rollback source = %q, want deployment_failure", rollbackEvent.Source)
			}
		})
	}
}

func TestHandleDeployResult_FailedRollbackIsNotRolledBack(t *testing.T) {
	setupDeployTestEnv(t)
	app := enableAutomaticRollback(t, seedDeployApp(t, models.Syncing).Id)
	target := seedRollbackTarget(t, app.Id)
	startRollbackEvent(t, app.Id, &target, "rollback-1")
	recorder := recordRollbacks(t)
	nop := zerolog.Nop()
	expectAsyncNotification(t)

	handleDeployResult(t.Context(), "agent-1", &messages.DeployResult{RequestId: "rollback-1", ApplicationId: app.Id, ErrorMessage: "boom"}, &nop)

	if len(recorder.targets) != 0 {
		t.Fatalf("a failed rollback must not be retried, got %v", recorder.targets)
	}
}

func TestHandleDeployResult_AutomaticRollbackKeepsError(t *testing.T) {
	setupDeployTestEnv(t)
	app := enableAutomaticRollback(t, seedDeployApp(t, models.Syncing).Id)
	target := seedRollbackTarget(t, app.Id)
	failure := "compose up failed"
	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).
		Update(t.Context(), "last_sync_error", failure); err != nil {
		t.Fatalf("set error: %v", err)
	}
	requestID := "auto-rollback"
	if _, err := applicationevents.Start(t.Context(), applicationevents.Params{
		ApplicationID: app.Id,
		RequestID:     &requestID,
		Type:          models.ApplicationEventRollback,
		Source:        models.ApplicationEventSourceDeploymentFailure,
		DeploymentID:  &target.Id,
	}); err != nil {
		t.Fatalf("start rollback event: %v", err)
	}
	nop := zerolog.Nop()
	expectAsyncNotification(t)

	handleDeployResult(t.Context(), "agent-1", &messages.DeployResult{RequestId: requestID, ApplicationId: app.Id, Success: true}, &nop)

	updated := loadDeployTestApp(t, app.Id)
	if updated.RolledBackAt == nil {
		t.Fatal("expected the application to be rolled back")
	}
	if updated.AutoSyncPaused {
		t.Error("expected auto-sync to stay active as configured")
	}
	if updated.LastSyncError == nil || *updated.LastSyncError != failure {
		t.Errorf("last sync error = %v, want the failure that was rolled back", updated.LastSyncError)
	}
}

func TestHandlePullImagesResult_FailedRedeployRollsBack(t *testing.T) {
	for _, redeployFailed := range []bool{true, false} {
		t.Run(map[bool]string{true: "redeploy failed", false: "pull failed"}[redeployFailed], func(t *testing.T) {
			setupDeployTestEnv(t)
			app := enableAutomaticRollback(t, seedDeployApp(t, models.Synced).Id)
			good := seedRollbackTarget(t, app.Id)
			if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).
				Update(t.Context(), "current_deployment_id", good.Id); err != nil {
				t.Fatalf("set current deployment: %v", err)
			}
			recorder := recordRollbacks(t)
			log := testLogger()
			expectAsyncNotification(t)

			handlePullImagesResult(t.Context(), &Client{Id: "agent-1"}, &messages.PullImagesResult{
				RequestId:      "image-req",
				ApplicationId:  app.Id,
				ErrorMessage:   "compose up after pull: container exited",
				RedeployFailed: redeployFailed,
			}, &log)

			if rolledBack := len(recorder.targets) == 1 && recorder.targets[0] == good.Id; rolledBack != redeployFailed {
				t.Errorf("rollbacks = %v, want rollback %v", recorder.targets, redeployFailed)
			}
		})
	}
}

func TestHandlePullImagesResult_HealthGate(t *testing.T) {
	setupDeployTestEnv(t)
	app := enableAutomaticRollback(t, seedDeployApp(t, models.Synced).Id)
	log := testLogger()

	handlePullImagesResult(t.Context(), &Client{Id: "agent-1"}, &messages.PullImagesResult{
		ApplicationId: app.Id,
		Success:       true,
		ImagesUpdated: true,
		Images:        []*messages.ServiceImage{{Service: "web", ImageId: "sha256:new"}},
	}, &log)

	deployment := verifyingDeployment(t, app.Id)
	if deployment == nil {
		t.Fatal("expected the image update to be verified")
	}
	event, err := gorm.G[models.ApplicationEvent](db.DB).Where("deployment_id = ?", deployment.Id).First(t.Context())
	if err != nil {
		t.Fatalf("load image update event: %v", err)
	}
	if event.Type != models.ApplicationEventImageUpdate || event.Status != models.ApplicationEventRunning {
		t.Errorf("unexpected image update event: %+v", event)
	}
}
