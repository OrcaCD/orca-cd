package deployments

import (
	"fmt"
	"path/filepath"
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
	})
	if err != nil {
		t.Fatalf("Succeed: %v", err)
	}
	if !found {
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
		found, err := Succeed(t.Context(), app.Id, requestID, nil)
		if err != nil {
			t.Fatalf("Succeed(%q): %v", requestID, err)
		}
		if found {
			t.Errorf("Succeed(%q) found a deployment", requestID)
		}
	}
	if found, _ := Succeed(t.Context(), "other-app", "req-1", nil); found {
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
		if _, err := Succeed(t.Context(), app.Id, requestID, nil); err != nil {
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

	if err := Discard(t.Context(), app.Id, "req-1"); err != nil {
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
	if _, err := Succeed(t.Context(), app.Id, "req-1", nil); err != nil {
		t.Fatalf("Succeed: %v", err)
	}

	if err := Discard(t.Context(), app.Id, "req-1"); err != nil {
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
	if _, err := Succeed(t.Context(), succeededApp.Id, "req-3", nil); err != nil {
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
	if _, err := Succeed(t.Context(), other.Id, "req-2", nil); err != nil {
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
	})
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
