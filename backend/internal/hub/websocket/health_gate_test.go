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

// connectAgents connects the agents for the health gate.
func connectAgents(t *testing.T, agentIDs ...string) map[string]*Client {
	t.Helper()
	log := testLogger()
	previous := DefaultHub
	DefaultHub = NewHub(&log)
	t.Cleanup(func() { DefaultHub = previous })
	clients := map[string]*Client{}
	for _, agentID := range agentIDs {
		conn := newTestWSConn(t)
		t.Cleanup(func() { _ = conn.Close() })
		client, err := DefaultHub.Register(agentID, conn)
		if err != nil {
			t.Fatalf("Register: %v", err)
		}
		clients[agentID] = client
	}
	return clients
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

func TestResolveHealthGates_FailingRollsBackAfterGrace(t *testing.T) {
	setupDeployTestEnv(t)
	app := enableAutomaticRollback(t, seedDeployApp(t, models.Syncing).Id)
	good := deployGated(t, &app)
	recorder := recordRollbacks(t)
	clients := connectAgents(t, "agent-1", "other-agent")
	deployedAt := *verifyingDeployment(t, app.Id).DeployedAt
	at := func(seconds int) time.Time { return deployedAt.Add(time.Duration(seconds) * time.Second) }
	nop := zerolog.Nop()
	expectAsyncNotification(t)

	clients["other-agent"].recordHealthReport(app.Id, models.Unhealthy, at(1))
	clients["agent-1"].recordHealthReport(app.Id, models.Unhealthy, at(1))
	clients["agent-1"].recordHealthReport(app.Id, models.Healthy, at(10))
	clients["agent-1"].recordHealthReport(app.Id, models.Unhealthy, at(20))
	clients["agent-1"].recordHealthReport(app.Id, models.Stopped, at(30))
	resolveHealthGates(t.Context(), at(49), &nop)
	if len(recorder.targets) != 0 || verifyingDeployment(t, app.Id) == nil {
		t.Fatal("a failing streak shorter than the grace period must not end the health check")
	}

	resolveHealthGates(t.Context(), at(50), &nop)

	if len(recorder.targets) != 1 || recorder.targets[0] != good.Id {
		t.Fatalf("expected a rollback to %q, got %v", good.Id, recorder.targets)
	}
	event := loadEventByRequest(t, "gated")
	if event.Status != models.ApplicationEventFailed || event.ErrorMessage == nil {
		t.Fatalf("unexpected deployment event: %+v", event)
	}
	if want := "health check failed: application was stopped for 30s after the deployment"; *event.ErrorMessage != want {
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

func TestFailHealthGate_WithoutEarlierDeployment(t *testing.T) {
	setupDeployTestEnv(t)
	app := enableAutomaticRollback(t, seedDeployApp(t, models.Syncing).Id)
	startDeployEvent(t, &app, "gated")
	nop := zerolog.Nop()
	handleDeployResult(t.Context(), "agent-1", &messages.DeployResult{RequestId: "gated", ApplicationId: app.Id, Success: true}, &nop)
	recorder := recordRollbacks(t)
	deployment := verifyingDeployment(t, app.Id)
	connectAgents(t, "agent-1")["agent-1"].recordHealthReport(app.Id, models.Unhealthy, *deployment.DeployedAt)
	expectAsyncNotification(t)

	resolveHealthGates(t.Context(), deployment.DeployedAt.Add(healthGateGrace), &nop)

	if len(recorder.targets) != 0 {
		t.Errorf("expected no rollback without an earlier deployment, got %v", recorder.targets)
	}
	if event := loadEventByRequest(t, "gated"); event.Status != models.ApplicationEventFailed {
		t.Errorf("event status = %q, want failed", event.Status)
	}
	if verifyingDeployment(t, app.Id) != nil {
		t.Error("expected the unhealthy deployment to be dropped")
	}
}

func TestStartHealthGate_Stops(t *testing.T) {
	nop := zerolog.Nop()
	stop := StartHealthGate(&nop)
	stop()
}

func TestResolveHealthGates_GateEnd(t *testing.T) {
	type report struct {
		health models.HealthStatus
		// before reports before the deployment instead of after it.
		before bool
	}
	tests := []struct {
		name         string
		connected    bool
		report       *report
		after        time.Duration
		wantEvent    models.ApplicationEventStatus
		wantError    string
		wantRollback bool
	}{
		{name: "healthy passes", connected: true, report: &report{health: models.Healthy}, after: time.Minute, wantEvent: models.ApplicationEventSucceeded},
		{
			name: "not healthy fails", connected: true, report: &report{health: models.UnknownHealth}, after: time.Minute,
			wantEvent: models.ApplicationEventFailed, wantError: "health check failed: application did not become healthy within 300s", wantRollback: true,
		},
		{name: "offline agent waits", after: time.Minute, wantEvent: models.ApplicationEventRunning},
		{name: "reconnected agent waits for its report", connected: true, after: time.Minute, wantEvent: models.ApplicationEventRunning},
		{name: "report from before the deployment is ignored", connected: true, report: &report{health: models.Healthy, before: true}, after: time.Minute, wantEvent: models.ApplicationEventRunning},
		{
			name: "missing report fails without rolling back", after: healthReportTimeout,
			wantEvent: models.ApplicationEventFailed, wantError: "health check failed: the agent did not report the application's health",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupDeployTestEnv(t)
			app := enableAutomaticRollback(t, seedDeployApp(t, models.Syncing).Id)
			good := deployGated(t, &app)
			recorder := recordRollbacks(t)
			deployment := verifyingDeployment(t, app.Id)
			if tt.connected {
				client := connectAgents(t, "agent-1")["agent-1"]
				if tt.report != nil {
					at := deployment.DeployedAt.Add(time.Second)
					if tt.report.before {
						at = deployment.DeployedAt.Add(-time.Second)
					}
					client.recordHealthReport(app.Id, tt.report.health, at)
				}
			}
			nop := zerolog.Nop()
			if tt.wantEvent != models.ApplicationEventRunning {
				expectAsyncNotification(t)
			}

			resolveHealthGates(t.Context(), deployment.VerifyUntil.Add(-time.Second), &nop)
			if event := loadEventByRequest(t, "gated"); event.Status != models.ApplicationEventRunning {
				t.Fatalf("a health check must not end before its timeout, got %q", event.Status)
			}
			resolveHealthGates(t.Context(), deployment.VerifyUntil.Add(tt.after), &nop)

			event := loadEventByRequest(t, "gated")
			if event.Status != tt.wantEvent {
				t.Errorf("event status = %q, want %q", event.Status, tt.wantEvent)
			}
			if got := len(recorder.targets) == 1 && recorder.targets[0] == good.Id; got != tt.wantRollback {
				t.Errorf("rolled back = %v, want %v", recorder.targets, tt.wantRollback)
			}
			if tt.wantError != "" && (event.ErrorMessage == nil || *event.ErrorMessage != tt.wantError) {
				t.Errorf("error = %v, want %q", event.ErrorMessage, tt.wantError)
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
	if updated.RolledBackReason == nil || *updated.RolledBackReason != failure {
		t.Errorf("rolled back reason = %v, want the failure that was rolled back", updated.RolledBackReason)
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

	connectAgents(t, "agent-1")["agent-1"].recordHealthReport(app.Id, models.Healthy, *deployment.DeployedAt)
	expectAsyncNotification(t)
	resolveHealthGates(t.Context(), *deployment.VerifyUntil, &log)

	event, err = gorm.G[models.ApplicationEvent](db.DB).Where("id = ?", event.Id).First(t.Context())
	if err != nil {
		t.Fatalf("reload image update event: %v", err)
	}
	if event.Status != models.ApplicationEventSucceeded {
		t.Errorf("image update event status = %q, want succeeded", event.Status)
	}
}

func TestHealthGate_EndsWhenTurnedOff(t *testing.T) {
	setupDeployTestEnv(t)
	app := enableAutomaticRollback(t, seedDeployApp(t, models.Syncing).Id)
	deployGated(t, &app)
	recorder := recordRollbacks(t)
	connectAgents(t, "agent-1")["agent-1"].recordHealthReport(app.Id, models.Unhealthy, time.Now())
	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).
		Update(t.Context(), "rollback_enabled", false); err != nil {
		t.Fatalf("disable rollback: %v", err)
	}
	nop := zerolog.Nop()
	expectAsyncNotification(t)

	resolveHealthGates(t.Context(), time.Now().Add(time.Minute), &nop)

	if event := loadEventByRequest(t, "gated"); event.Status != models.ApplicationEventSucceeded {
		t.Errorf("event status = %q, want succeeded once the health check is off", event.Status)
	}
	if len(recorder.targets) != 0 {
		t.Errorf("expected no rollback, got %v", recorder.targets)
	}
	if verifyingDeployment(t, app.Id) != nil {
		t.Error("expected the health check to end")
	}
}

func TestHealthReports(t *testing.T) {
	setupDeployTestEnv(t)
	app := seedAppForAgent(t, "agent-1")
	client := connectAgents(t, "agent-1")["agent-1"]
	if _, ok := DefaultHub.lastHealthReport("agent-1", app.Id); ok {
		t.Fatal("a new connection has not reported yet")
	}
	nop := zerolog.Nop()

	handleApplicationStatusReport(t.Context(), client, &messages.ApplicationStatusReport{
		Statuses: []*messages.ApplicationStatus{{ApplicationId: app.Id, Health: messages.HealthStatus_HEALTH_STATUS_HEALTHY}},
	}, &nop)

	healthy, ok := DefaultHub.lastHealthReport("agent-1", app.Id)
	if !ok || healthy.health != models.Healthy || !healthy.failingSince.IsZero() {
		t.Fatalf("expected the report to count for the connection, got %+v, %v", healthy, ok)
	}
	if _, ok := DefaultHub.lastHealthReport("agent-2", app.Id); ok {
		t.Error("another agent did not report")
	}

	start := time.Now()
	client.recordHealthReport(app.Id, models.Unhealthy, start)
	client.recordHealthReport(app.Id, models.Degraded, start.Add(time.Second))
	if got, _ := DefaultHub.lastHealthReport("agent-1", app.Id); !got.failingSince.Equal(start) {
		t.Errorf("expected the failing streak to continue from %v, got %+v", start, got)
	}
	client.recordHealthReport(app.Id, models.Healthy, start.Add(2*time.Second))
	client.recordHealthReport(app.Id, models.Stopped, start.Add(3*time.Second))
	if got, _ := DefaultHub.lastHealthReport("agent-1", app.Id); !got.failingSince.Equal(start.Add(3 * time.Second)) {
		t.Errorf("expected a healthy report to end the failing streak, got %+v", got)
	}

	client.Close()
	if _, ok := DefaultHub.lastHealthReport("agent-1", app.Id); ok {
		t.Error("a closing connection's reports must not count")
	}
}
