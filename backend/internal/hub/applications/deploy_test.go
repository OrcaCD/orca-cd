package applications

import (
	"context"
	"errors"
	"testing"

	application_deployer "github.com/OrcaCD/orca-cd/internal/hub/deployer"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
)

// ctxDeployer records whether the context of the deploy was already done.
type ctxDeployer struct {
	stubDeployer
	ctxErr error
}

func (d *ctxDeployer) TriggerApplicationDeploy(ctx context.Context, app *models.Application, composeFile, requestID string) error {
	d.ctxErr = ctx.Err()
	return d.stubDeployer.TriggerApplicationDeploy(ctx, app, composeFile, requestID)
}

func TestDeploy_RecordsEventAndDeploysStoredCompose(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	agent := seedAgent(t)
	app := seedApp(t, repo.Id, agent.Id, "services: {}\n")

	stub := &stubDeployer{}
	application_deployer.DefaultApplicationDeployer = stub
	t.Cleanup(func() { application_deployer.DefaultApplicationDeployer = nil })

	actorID, actorName := "user-1", "Test User"
	if err := Deploy(t.Context(), &app, SyncOrigin{Source: models.ApplicationEventSourceManual, ActorUserID: &actorID, ActorName: &actorName}); err != nil {
		t.Fatalf("Deploy() error: %v", err)
	}

	if stub.compose != "services: {}\n" {
		t.Errorf("deployed compose = %q", stub.compose)
	}
	events := loadAppEvents(t, app.Id)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	event := events[0]
	if event.RequestId == nil || *event.RequestId != stub.requestID ||
		event.Type != models.ApplicationEventDeployment ||
		event.Status != models.ApplicationEventRunning ||
		event.ActorName == nil || *event.ActorName != actorName {
		t.Errorf("unexpected event: %+v", event)
	}
}

// The deploy reaches the agent even when the client is gone, so it must still
// be recorded completely.
func TestDeploy_IgnoresCanceledContext(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	agent := seedAgent(t)
	app := seedApp(t, repo.Id, agent.Id, "services: {}\n")

	deployer := &ctxDeployer{}
	application_deployer.DefaultApplicationDeployer = deployer
	t.Cleanup(func() { application_deployer.DefaultApplicationDeployer = nil })

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Deploy(ctx, &app, SyncOrigin{Source: models.ApplicationEventSourceManual}); err != nil {
		t.Fatalf("Deploy() error: %v", err)
	}

	if deployer.ctxErr != nil {
		t.Errorf("deployer got a done context: %v", deployer.ctxErr)
	}
	if events := loadAppEvents(t, app.Id); len(events) != 1 {
		t.Errorf("expected the deployment event to be recorded, got %d events", len(events))
	}
}

func TestDeploy_WithoutDeployer(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	agent := seedAgent(t)
	app := seedApp(t, repo.Id, agent.Id, "services: {}\n")

	application_deployer.DefaultApplicationDeployer = nil
	if err := Deploy(t.Context(), &app, SyncOrigin{Source: models.ApplicationEventSourceManual}); !errors.Is(err, ErrDeployerNotInitialized) {
		t.Errorf("Deploy() error = %v, want %v", err, ErrDeployerNotInitialized)
	}
}
