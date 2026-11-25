package rollback

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/applicationevents"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	application_deployer "github.com/OrcaCD/orca-cd/internal/hub/deployer"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type testDeployer struct {
	rollback func(context.Context, *models.Application, *models.ApplicationDeployment, string) error
}

func (testDeployer) TriggerApplicationDeploy(context.Context, *models.Application, string, string) error {
	return nil
}

func (d testDeployer) TriggerRollback(ctx context.Context, app *models.Application, target *models.ApplicationDeployment, requestID string) error {
	return d.rollback(ctx, app, target, requestID)
}

func TestTriggerRecordsEventBeforeDispatch(t *testing.T) {
	testDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "rollback.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	previousDB := db.DB
	db.DB = testDB
	previousDeployer := application_deployer.DefaultApplicationDeployer
	t.Cleanup(func() {
		application_deployer.DefaultApplicationDeployer = previousDeployer
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
		db.DB = previousDB
	})
	if err := testDB.AutoMigrate(&models.ApplicationEvent{}); err != nil {
		t.Fatalf("migrate events: %v", err)
	}

	app := models.Application{Id: "app"}
	target := models.ApplicationDeployment{Id: "previous-deployment", Commit: "restored"}
	const triggerID = "workflow-trigger"
	var dispatched bool
	application_deployer.DefaultApplicationDeployer = testDeployer{
		rollback: func(ctx context.Context, app *models.Application, target *models.ApplicationDeployment, requestID string) error {
			dispatched = true
			event, err := applicationevents.ForRequest(ctx, requestID, app.Id)
			if err != nil || event == nil {
				t.Fatalf("expected rollback event before dispatch, got %+v, %v", event, err)
			}
			if event.Type != models.ApplicationEventRollback || event.Status != models.ApplicationEventRunning ||
				event.TriggerId == nil || *event.TriggerId != triggerID ||
				event.DeploymentId == nil || *event.DeploymentId != target.Id ||
				event.CommitHash == nil || *event.CommitHash != target.Commit {
				t.Errorf("unexpected rollback event: %+v", event)
			}
			return nil
		},
	}

	if err := Trigger(t.Context(), &app, &target, Origin{
		Source: models.ApplicationEventSourceHealthCheck, TriggerID: new(triggerID),
	}); err != nil {
		t.Fatalf("trigger rollback: %v", err)
	}
	if !dispatched {
		t.Error("expected rollback dispatch")
	}
}
