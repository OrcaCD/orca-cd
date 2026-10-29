package deployments

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func setupTestDB(t *testing.T) {
	t.Helper()

	testDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "deployments.db")), &gorm.Config{
		Logger: gormlogger.Discard,
	})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := testDB.AutoMigrate(&models.Application{}, &models.ApplicationEvent{}, &models.ApplicationDeployment{}); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	sqlDB, err := testDB.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	previousDB := db.DB
	db.DB = testDB
	t.Cleanup(func() {
		db.DB = previousDB
		_ = sqlDB.Close()
	})

	if err := crypto.Init("test-secret-that-is-long-enough-32chars"); err != nil {
		t.Fatalf("init crypto: %v", err)
	}
}

func seedApp(t *testing.T, agentID string) models.Application {
	t.Helper()
	app := models.Application{
		Name:          crypto.EncryptedString("app"),
		RepositoryId:  "repository-1",
		AgentId:       agentID,
		SyncStatus:    models.Synced,
		HealthStatus:  models.Healthy,
		Branch:        "main",
		Commit:        "c1",
		CommitMessage: "first commit",
		Path:          "compose.yaml",
		ComposeFile:   crypto.EncryptedString("services: {}\n"),
	}
	if err := db.DB.Select("*").Create(&app).Error; err != nil {
		t.Fatalf("seed application: %v", err)
	}
	return app
}

func seedEvent(t *testing.T, appID, requestID string) models.ApplicationEvent {
	t.Helper()
	event := models.ApplicationEvent{
		ApplicationId: appID,
		RequestId:     &requestID,
		Type:          models.ApplicationEventDeployment,
		Source:        models.ApplicationEventSourceManual,
		Status:        models.ApplicationEventRunning,
	}
	if err := gorm.G[models.ApplicationEvent](db.DB).Create(t.Context(), &event); err != nil {
		t.Fatalf("seed event: %v", err)
	}
	return event
}

func loadApp(t *testing.T, id string) models.Application {
	t.Helper()
	app, err := gorm.G[models.Application](db.DB).Where("id = ?", id).First(t.Context())
	if err != nil {
		t.Fatalf("load application: %v", err)
	}
	return app
}

func loadEvent(t *testing.T, id string) models.ApplicationEvent {
	t.Helper()
	event, err := gorm.G[models.ApplicationEvent](db.DB).Where("id = ?", id).First(t.Context())
	if err != nil {
		t.Fatalf("load event: %v", err)
	}
	return event
}

func deploymentsOf(t *testing.T, appID string) []models.ApplicationDeployment {
	t.Helper()
	deployments, err := gorm.G[models.ApplicationDeployment](db.DB).
		Where("application_id = ?", appID).
		Order("created_at, id").
		Find(t.Context())
	if err != nil {
		t.Fatalf("load deployments: %v", err)
	}
	return deployments
}

func startDeployment(t *testing.T, app *models.Application, requestID string) *models.ApplicationDeployment {
	t.Helper()
	deployment, err := Start(t.Context(), app, "services:\n  web:\n    image: nginx\n", requestID)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return deployment
}

func TestStartRecordsPendingDeploymentAndLinksEvent(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t, "agent-1")
	event := seedEvent(t, app.Id, "req-1")

	deployment := startDeployment(t, &app, "req-1")

	if deployment.Status != models.DeploymentPending {
		t.Errorf("status = %q, want pending", deployment.Status)
	}
	if deployment.Commit != "c1" || deployment.CommitMessage != "first commit" {
		t.Errorf("commit = %q %q, want the application's commit", deployment.Commit, deployment.CommitMessage)
	}
	if got := deployment.ComposeFile.String(); got != "services:\n  web:\n    image: nginx\n" {
		t.Errorf("compose file = %q, want the dispatched one", got)
	}
	if got := loadEvent(t, event.Id).DeploymentId; got == nil || *got != deployment.Id {
		t.Errorf("event deployment = %v, want %q", got, deployment.Id)
	}
	if loadApp(t, app.Id).CurrentDeploymentId != nil {
		t.Error("a pending deployment must not become the current one")
	}
}

func TestStartDropsEarlierPendingDeployments(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t, "agent-1")
	other := seedApp(t, "agent-1")
	staleEvent := seedEvent(t, app.Id, "req-1")
	startDeployment(t, &app, "req-1")
	otherPending := startDeployment(t, &other, "req-other")

	latest := startDeployment(t, &app, "req-2")

	deployments := deploymentsOf(t, app.Id)
	if len(deployments) != 1 || deployments[0].Id != latest.Id {
		t.Fatalf("expected only the latest pending deployment, got %v", deployments)
	}
	if got := loadEvent(t, staleEvent.Id).DeploymentId; got != nil {
		t.Errorf("expected the dropped deployment to be unlinked from its event, got %q", *got)
	}
	if got := deploymentsOf(t, other.Id); len(got) != 1 || got[0].Id != otherPending.Id {
		t.Errorf("expected other applications to keep their pending deployment, got %v", got)
	}
}

func TestSucceedRecordsImagesAndBecomesCurrent(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t, "agent-1")
	deployment := startDeployment(t, &app, "req-1")

	found, err := Succeed(t.Context(), app.Id, "req-1", []*messages.ServiceImage{
		{Service: "web", ImageId: "sha256:web", PinnedRef: "nginx@sha256:aaa"},
		{Service: "", ImageId: "sha256:ignored"},
		{Service: "worker", ImageId: ""},
	}, 0)
	if err != nil {
		t.Fatalf("Succeed: %v", err)
	}
	if found == nil {
		t.Fatal("expected the pending deployment to be found")
	}

	got := deploymentsOf(t, app.Id)[0]
	if got.Status != models.DeploymentSucceeded {
		t.Errorf("status = %q, want succeeded", got.Status)
	}
	if got.DeployedAt == nil {
		t.Error("expected deployed_at to be set")
	}
	want := []models.DeployedImage{{Service: "web", ImageId: "sha256:web", PinnedRef: "nginx@sha256:aaa"}}
	if len(got.Images) != 1 || got.Images[0] != want[0] {
		t.Errorf("images = %v, want %v", got.Images, want)
	}
	if current := loadApp(t, app.Id).CurrentDeploymentId; current == nil || *current != deployment.Id {
		t.Errorf("current deployment = %v, want %q", current, deployment.Id)
	}
}

func TestSucceedIgnoresUnknownRequests(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t, "agent-1")
	startDeployment(t, &app, "req-1")

	for _, requestID := range []string{"", "req-unknown"} {
		found, err := Succeed(t.Context(), app.Id, requestID, nil, 0)
		if err != nil {
			t.Fatalf("Succeed(%q): %v", requestID, err)
		}
		if found != nil {
			t.Errorf("Succeed(%q) found a deployment", requestID)
		}
	}
	if found, _ := Succeed(t.Context(), "other-app", "req-1", nil, 0); found != nil {
		t.Error("a request of another application must not match")
	}
	if loadApp(t, app.Id).CurrentDeploymentId != nil {
		t.Error("expected no current deployment")
	}
}

func TestSucceedPrunesOldDeployments(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t, "agent-1")

	var ids []string
	for i := range MaxPerApplication + 2 {
		requestID := fmt.Sprintf("req-%d", i)
		seedEvent(t, app.Id, requestID)
		deployment := startDeployment(t, &app, requestID)
		if _, err := Succeed(t.Context(), app.Id, requestID, nil, 0); err != nil {
			t.Fatalf("Succeed: %v", err)
		}
		ids = append(ids, deployment.Id)
		// deployed_at orders the deployments.
		time.Sleep(2 * time.Millisecond)
	}

	remaining := deploymentsOf(t, app.Id)
	if len(remaining) != MaxPerApplication {
		t.Fatalf("expected %d deployments, got %d", MaxPerApplication, len(remaining))
	}
	for _, deployment := range remaining {
		if deployment.Id == ids[0] || deployment.Id == ids[1] {
			t.Errorf("expected the oldest deployment %s to be pruned", deployment.Id)
		}
	}
	events, err := gorm.G[models.ApplicationEvent](db.DB).Where("deployment_id IN ?", ids[:2]).Find(t.Context())
	if err != nil {
		t.Fatalf("load events: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected pruned deployments to be unlinked from their events, got %d", len(events))
	}
}

func TestDiscardDropsPendingDeployment(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t, "agent-1")
	event := seedEvent(t, app.Id, "req-1")
	startDeployment(t, &app, "req-1")

	if _, err := Discard(t.Context(), app.Id, "req-1"); err != nil {
		t.Fatalf("Discard: %v", err)
	}

	if got := deploymentsOf(t, app.Id); len(got) != 0 {
		t.Errorf("expected no deployments, got %v", got)
	}
	if got := loadEvent(t, event.Id).DeploymentId; got != nil {
		t.Errorf("expected the event to be unlinked, got %q", *got)
	}
}

func TestDiscardKeepsSucceededDeployment(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t, "agent-1")
	startDeployment(t, &app, "req-1")
	if _, err := Succeed(t.Context(), app.Id, "req-1", nil, 0); err != nil {
		t.Fatalf("Succeed: %v", err)
	}

	if _, err := Discard(t.Context(), app.Id, "req-1"); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	if got := deploymentsOf(t, app.Id); len(got) != 1 {
		t.Errorf("expected the succeeded deployment to stay, got %v", got)
	}
}

func TestDiscardPendingForAgent(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t, "agent-1")
	otherAgentApp := seedApp(t, "agent-2")
	succeededApp := seedApp(t, "agent-1")
	startDeployment(t, &app, "req-1")
	startDeployment(t, &otherAgentApp, "req-2")
	startDeployment(t, &succeededApp, "req-3")
	if _, err := Succeed(t.Context(), succeededApp.Id, "req-3", nil, 0); err != nil {
		t.Fatalf("Succeed: %v", err)
	}

	if err := DiscardPendingForAgent(t.Context(), "agent-1"); err != nil {
		t.Fatalf("DiscardPendingForAgent: %v", err)
	}

	if got := deploymentsOf(t, app.Id); len(got) != 0 {
		t.Errorf("expected the agent's pending deployment to be dropped, got %v", got)
	}
	if got := deploymentsOf(t, otherAgentApp.Id); len(got) != 1 {
		t.Errorf("expected other agents' deployments to stay, got %v", got)
	}
	if got := deploymentsOf(t, succeededApp.Id); len(got) != 1 {
		t.Errorf("expected succeeded deployments to stay, got %v", got)
	}
}

func TestDiscardAllPending(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t, "agent-1")
	other := seedApp(t, "agent-2")
	startDeployment(t, &app, "req-1")
	startDeployment(t, &other, "req-2")
	if _, err := Succeed(t.Context(), other.Id, "req-2", nil, 0); err != nil {
		t.Fatalf("Succeed: %v", err)
	}

	if err := DiscardAllPending(t.Context()); err != nil {
		t.Fatalf("DiscardAllPending: %v", err)
	}

	if got := deploymentsOf(t, app.Id); len(got) != 0 {
		t.Errorf("expected pending deployments to be dropped, got %v", got)
	}
	if got := deploymentsOf(t, other.Id); len(got) != 1 {
		t.Errorf("expected succeeded deployments to stay, got %v", got)
	}
}

func TestRecordImageUpdate(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t, "agent-1")
	event := seedEvent(t, app.Id, "req-image")

	deployment, err := RecordImageUpdate(t.Context(), app.Id, "req-image", []*messages.ServiceImage{
		{Service: "web", ImageId: "sha256:new", PinnedRef: "nginx@sha256:new"},
	}, 0)
	if err != nil {
		t.Fatalf("RecordImageUpdate: %v", err)
	}

	if deployment.Status != models.DeploymentSucceeded || deployment.DeployedAt == nil {
		t.Errorf("expected a succeeded deployment, got %+v", deployment)
	}
	if deployment.Commit != "c1" || deployment.ComposeFile.String() != "services: {}\n" {
		t.Errorf("expected the application's commit and compose file, got %q %q", deployment.Commit, deployment.ComposeFile.String())
	}
	if len(deployment.Images) != 1 || deployment.Images[0].ImageId != "sha256:new" {
		t.Errorf("images = %v", deployment.Images)
	}
	if got := loadEvent(t, event.Id).DeploymentId; got == nil || *got != deployment.Id {
		t.Errorf("event deployment = %v, want %q", got, deployment.Id)
	}
	if current := loadApp(t, app.Id).CurrentDeploymentId; current == nil || *current != deployment.Id {
		t.Errorf("current deployment = %v, want %q", current, deployment.Id)
	}
}

func succeedDeployment(t *testing.T, app *models.Application, requestID string, gate time.Duration, images ...*messages.ServiceImage) *models.ApplicationDeployment {
	t.Helper()
	startDeployment(t, app, requestID)
	deployment, err := Succeed(t.Context(), app.Id, requestID, images, gate)
	if err != nil {
		t.Fatalf("Succeed: %v", err)
	}
	if deployment == nil {
		t.Fatal("expected a deployment")
	}
	// deployed_at orders the deployments.
	time.Sleep(2 * time.Millisecond)
	return deployment
}

func setHealth(t *testing.T, appID string, health models.HealthStatus) {
	t.Helper()
	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", appID).Update(t.Context(), "health_status", health); err != nil {
		t.Fatalf("set health: %v", err)
	}
}

func TestStartRecordsPreviousDeployment(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t, "agent-1")
	first := succeedDeployment(t, &app, "req-1", 0)

	second := startDeployment(t, &app, "req-2")

	if second.PreviousDeploymentId == nil || *second.PreviousDeploymentId != first.Id {
		t.Errorf("previous deployment = %v, want %q", second.PreviousDeploymentId, first.Id)
	}
}

func TestSucceedWithHealthGate(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t, "agent-1")
	first := succeedDeployment(t, &app, "req-1", 0)

	gated := succeedDeployment(t, &app, "req-2", 5*time.Minute)

	if gated.Status != models.DeploymentVerifying {
		t.Errorf("status = %q, want verifying", gated.Status)
	}
	if gated.VerifyUntil == nil || gated.VerifyUntil.Sub(*gated.DeployedAt) != 5*time.Minute {
		t.Errorf("verify until = %v, want 5m after %v", gated.VerifyUntil, gated.DeployedAt)
	}
	if current := loadApp(t, app.Id).CurrentDeploymentId; current == nil || *current != gated.Id {
		t.Errorf("current deployment = %v, want the verifying one", current)
	}
	target, err := RollbackTarget(t.Context(), &app)
	if err != nil {
		t.Fatalf("RollbackTarget: %v", err)
	}
	if target == nil || target.Id != first.Id {
		t.Errorf("rollback target = %v, want the deployment before the verifying one", target)
	}

	due, err := VerificationsDue(t.Context(), time.Now())
	if err != nil {
		t.Fatalf("VerificationsDue: %v", err)
	}
	if len(due) != 0 {
		t.Errorf("expected no due verification yet, got %v", due)
	}
	due, err = VerificationsDue(t.Context(), time.Now().Add(6*time.Minute))
	if err != nil {
		t.Fatalf("VerificationsDue: %v", err)
	}
	if len(due) != 1 || due[0].Id != gated.Id {
		t.Errorf("expected the gated deployment to be due, got %v", due)
	}
}

func TestVerifyAndReject(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t, "agent-1")
	verified := succeedDeployment(t, &app, "req-1", time.Minute)

	if ok, err := Verify(t.Context(), verified); err != nil || !ok {
		t.Fatalf("Verify = %v, %v", ok, err)
	}
	if ok, _ := Verify(t.Context(), verified); ok {
		t.Error("a deployment must only be verified once")
	}
	if ok, _ := Reject(t.Context(), verified); ok {
		t.Error("a verified deployment must not be rejected")
	}

	event := seedEvent(t, app.Id, "req-2")
	rejected := succeedDeployment(t, &app, "req-2", time.Minute)
	if ok, err := Reject(t.Context(), rejected); err != nil || !ok {
		t.Fatalf("Reject = %v, %v", ok, err)
	}
	if got := deploymentsOf(t, app.Id); len(got) != 1 || got[0].Id != verified.Id || got[0].Status != models.DeploymentSucceeded {
		t.Errorf("expected only the verified deployment to remain, got %v", got)
	}
	if loadApp(t, app.Id).CurrentDeploymentId != nil {
		t.Error("expected no current deployment after rejecting the running one")
	}
	if loadEvent(t, event.Id).DeploymentId != nil {
		t.Error("expected the rejected deployment to be unlinked from its event")
	}
}

func TestSupersede(t *testing.T) {
	for _, health := range []models.HealthStatus{models.Healthy, models.UnknownHealth} {
		t.Run(string(health), func(t *testing.T) {
			setupTestDB(t)
			app := seedApp(t, "agent-1")
			event := seedEvent(t, app.Id, "req-1")
			gated := succeedDeployment(t, &app, "req-1", time.Minute)
			setHealth(t, app.Id, health)

			next := startDeployment(t, &app, "req-2")

			completed := loadEvent(t, event.Id)
			deployments := deploymentsOf(t, app.Id)
			if health == models.Healthy {
				if completed.Status != models.ApplicationEventSucceeded {
					t.Errorf("event status = %q, want succeeded", completed.Status)
				}
				if len(deployments) != 2 || deployments[0].Status != models.DeploymentSucceeded {
					t.Errorf("expected the replaced deployment to succeed, got %v", deployments)
				}
				if next.PreviousDeploymentId == nil || *next.PreviousDeploymentId != gated.Id {
					t.Errorf("previous deployment = %v, want %q", next.PreviousDeploymentId, gated.Id)
				}
				return
			}
			if completed.Status != models.ApplicationEventFailed || completed.ErrorMessage == nil || *completed.ErrorMessage != supersededError {
				t.Errorf("unexpected event: %+v", completed)
			}
			if len(deployments) != 1 || deployments[0].Id != next.Id {
				t.Errorf("expected the replaced deployment to be dropped, got %v", deployments)
			}
		})
	}
}

func TestImageRetention(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t, "agent-1")
	succeedDeployment(t, &app, "req-1", 0, &messages.ServiceImage{Service: "web", ImageId: "sha256:oldest"})
	succeedDeployment(t, &app, "req-2", 0, &messages.ServiceImage{Service: "web", ImageId: "sha256:good"}, &messages.ServiceImage{Service: "db", ImageId: "sha256:db"})
	succeedDeployment(t, &app, "req-3", time.Minute, &messages.ServiceImage{Service: "web", ImageId: "sha256:new"}, &messages.ServiceImage{Service: "db", ImageId: "sha256:db"})
	app = loadApp(t, app.Id)

	if retention, err := ImageRetention(t.Context(), &app); err != nil || retention != nil {
		t.Fatalf("expected no retention without automatic rollback, got %v, %v", retention, err)
	}

	app.RollbackEnabled = true
	app.DeleteOldImages = true
	retention, err := ImageRetention(t.Context(), &app)
	if err != nil {
		t.Fatalf("ImageRetention: %v", err)
	}
	keep := retention.GetKeepImageIds()
	slices.Sort(keep)
	if want := []string{"sha256:db", "sha256:good", "sha256:new"}; !slices.Equal(keep, want) {
		t.Errorf("keep = %v, want %v", keep, want)
	}
	if want := []string{"sha256:oldest"}; !slices.Equal(retention.GetRemoveImageIds(), want) {
		t.Errorf("remove = %v, want %v", retention.GetRemoveImageIds(), want)
	}
}
