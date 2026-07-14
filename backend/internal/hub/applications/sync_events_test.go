package applications

import (
	"context"
	"errors"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	application_deployer "github.com/OrcaCD/orca-cd/internal/hub/deployer"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/repositories"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

func loadAppEvents(t *testing.T, applicationID string) []models.ApplicationEvent {
	t.Helper()
	events, err := gorm.G[models.ApplicationEvent](db.DB).
		Where("application_id = ?", applicationID).
		Order("created_at ASC").
		Find(t.Context())
	if err != nil {
		t.Fatalf("failed to load application events: %v", err)
	}
	return events
}

func TestProcessSyncJobRepositoryPollingRecordedCommitPreservesFailureState(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	agent := seedAgent(t)
	const compose = "services: {}\n"
	app := seedApp(t, repo.Id, agent.Id, compose)
	previousError := "previous failure"
	if _, err := gorm.G[models.Application](db.DB).
		Where("id = ?", app.Id).
		Select("SyncStatus", "LastSyncError").
		Updates(t.Context(), models.Application{
			SyncStatus:    models.OutOfSync,
			LastSyncError: &previousError,
		}); err != nil {
		t.Fatalf("failed to set application pre-state: %v", err)
	}

	fetched := false
	provider := &mockProvider{fileContent: compose, onGetFileContent: func() { fetched = true }}
	nop := zerolog.Nop()
	processSyncJob(t.Context(), syncJob{
		Application: app, Repository: repo, RepositoryProvider: provider,
		Commit: app.Commit, CommitMessage: "same",
		Origin: SyncOrigin{Source: models.ApplicationEventSourceRepositoryPolling},
	}, &nop)

	if fetched {
		t.Error("expected compose file fetch to be skipped for a recorded commit")
	}
	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load application: %v", err)
	}
	if updated.SyncStatus != models.OutOfSync {
		t.Errorf("expected SyncStatus %q, got %q", models.OutOfSync, updated.SyncStatus)
	}
	if updated.LastSyncedAt != nil {
		t.Errorf("expected LastSyncedAt to remain nil, got %v", updated.LastSyncedAt)
	}
	if updated.LastSyncError == nil || *updated.LastSyncError != previousError {
		t.Errorf("expected LastSyncError %q, got %v", previousError, updated.LastSyncError)
	}
	if events := loadAppEvents(t, app.Id); len(events) != 0 {
		t.Fatalf("expected no events for a recorded commit poll, got %d", len(events))
	}
}

func TestProcessSyncJobPollingNewCommitUnchangedComposeRecordsNoChange(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	agent := seedAgent(t)
	const compose = "services: {}\n"
	app := seedApp(t, repo.Id, agent.Id, compose)

	provider := &mockProvider{fileContent: compose}
	nop := zerolog.Nop()
	processSyncJob(t.Context(), syncJob{
		Application: app, Repository: repo, RepositoryProvider: provider,
		Commit: "new-sha", CommitMessage: "new commit",
		Origin: SyncOrigin{Source: models.ApplicationEventSourceRepositoryPolling},
	}, &nop)

	events := loadAppEvents(t, app.Id)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	event := events[0]
	if event.Type != models.ApplicationEventCommitSync ||
		event.Source != models.ApplicationEventSourceRepositoryPolling ||
		event.Status != models.ApplicationEventNoChange {
		t.Fatalf("unexpected event: %+v", event)
	}
	if event.CommitHash == nil || *event.CommitHash != "new-sha" {
		t.Errorf("expected commit hash %q, got %v", "new-sha", event.CommitHash)
	}
	if event.CompletedAt == nil {
		t.Error("expected no_change event to be completed")
	}
}

func TestProcessSyncJobUnchangedComposeStatusUpdateFailureRecordsFailedEvent(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	agent := seedAgent(t)
	const compose = "services: {}\n"
	app := seedApp(t, repo.Id, agent.Id, compose)

	if err := db.DB.Exec(`CREATE TRIGGER fail_application_status_update
		BEFORE UPDATE ON applications
		BEGIN
			SELECT RAISE(FAIL, 'forced application update failure');
		END`).Error; err != nil {
		t.Fatalf("failed to create update trigger: %v", err)
	}

	provider := &mockProvider{fileContent: compose}
	nop := zerolog.Nop()
	processSyncJob(t.Context(), syncJob{
		Application: app, Repository: repo, RepositoryProvider: provider,
		Commit: "new-sha", CommitMessage: "new commit",
		Origin: SyncOrigin{Source: models.ApplicationEventSourceRepositoryPolling},
	}, &nop)

	events := loadAppEvents(t, app.Id)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Status != models.ApplicationEventFailed || events[0].ErrorMessage == nil {
		t.Fatalf("expected failed event with error message, got %+v", events[0])
	}
	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load application: %v", err)
	}
	if updated.Commit != app.Commit {
		t.Fatalf("expected commit to remain %q, got %q", app.Commit, updated.Commit)
	}
}

func TestProcessSyncJobManualIdenticalCommitRecordsNoChangeWithActor(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	agent := seedAgent(t)
	const compose = "services: {}\n"
	app := seedApp(t, repo.Id, agent.Id, compose)

	actorID, actorName := "user-1", "Alex"
	provider := &mockProvider{fileContent: compose}
	nop := zerolog.Nop()
	processSyncJob(t.Context(), syncJob{
		Application: app, Repository: repo, RepositoryProvider: provider,
		Commit: app.Commit, CommitMessage: app.CommitMessage,
		Origin: SyncOrigin{
			Source:      models.ApplicationEventSourceManual,
			ActorUserID: &actorID,
			ActorName:   &actorName,
		},
	}, &nop)

	events := loadAppEvents(t, app.Id)
	if len(events) != 1 {
		t.Fatalf("expected 1 event for explicit no-op sync, got %d", len(events))
	}
	event := events[0]
	if event.Status != models.ApplicationEventNoChange || event.Source != models.ApplicationEventSourceManual {
		t.Fatalf("unexpected event: %+v", event)
	}
	if event.ActorUserId == nil || *event.ActorUserId != actorID {
		t.Errorf("expected actor user id %q, got %v", actorID, event.ActorUserId)
	}
	if event.ActorName == nil || *event.ActorName != actorName {
		t.Errorf("expected actor name %q, got %v", actorName, event.ActorName)
	}
}

func TestProcessSyncJobComposeChangedPassesEventRequestIDToDeployer(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	agent := seedAgent(t)
	app := seedApp(t, repo.Id, agent.Id, "services: {}\n")

	stub := &stubDeployer{}
	prev := application_deployer.DefaultApplicationDeployer
	application_deployer.DefaultApplicationDeployer = stub
	t.Cleanup(func() { application_deployer.DefaultApplicationDeployer = prev })

	provider := &mockProvider{fileContent: "services:\n  app: {}\n"}
	nop := zerolog.Nop()
	processSyncJob(t.Context(), syncJob{
		Application: app, Repository: repo, RepositoryProvider: provider,
		Commit: "new-sha", CommitMessage: "new commit",
		Origin: SyncOrigin{Source: models.ApplicationEventSourceRepositoryWebhook},
	}, &nop)

	if stub.requestID == "" {
		t.Fatal("expected deployer to receive the event request id")
	}
	event, err := gorm.G[models.ApplicationEvent](db.DB).
		Where("request_id = ?", stub.requestID).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load event by request id: %v", err)
	}
	if event.Status != models.ApplicationEventRunning {
		t.Fatalf("expected running event awaiting deploy result, got %q", event.Status)
	}
	if event.Type != models.ApplicationEventCommitSync ||
		event.Source != models.ApplicationEventSourceRepositoryWebhook {
		t.Fatalf("unexpected event: %+v", event)
	}
}

func TestProcessSyncJobFetchFailureRecordsFailedEvent(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	agent := seedAgent(t)
	app := seedApp(t, repo.Id, agent.Id, "services: {}\n")

	provider := &mockProvider{fileContentErr: errors.New("boom")}
	nop := zerolog.Nop()
	processSyncJob(t.Context(), syncJob{
		Application: app, Repository: repo, RepositoryProvider: provider,
		Commit: "new-sha", CommitMessage: "new commit",
		Origin: SyncOrigin{Source: models.ApplicationEventSourceGitHubActions},
	}, &nop)

	events := loadAppEvents(t, app.Id)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	event := events[0]
	if event.Status != models.ApplicationEventFailed || event.ErrorMessage == nil {
		t.Fatalf("expected failed event with error message, got %+v", event)
	}
}

func TestProcessSyncJobCanceledContextStillCompletesFailedEvent(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	agent := seedAgent(t)
	app := seedApp(t, repo.Id, agent.Id, "services: {}\n")

	ctx, cancel := context.WithCancel(t.Context())
	provider := &mockProvider{
		fileContentErr:   errors.New("request canceled"),
		onGetFileContent: cancel,
	}
	nop := zerolog.Nop()
	processSyncJob(ctx, syncJob{
		Application: app, Repository: repo, RepositoryProvider: provider,
		Commit: "new-sha", CommitMessage: "new commit",
		Origin: SyncOrigin{Source: models.ApplicationEventSourceRepositoryWebhook},
	}, &nop)

	events := loadAppEvents(t, app.Id)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Status != models.ApplicationEventFailed {
		t.Fatalf("expected canceled sync event to be failed, got %+v", events[0])
	}
	if events[0].CompletedAt == nil {
		t.Fatal("expected canceled sync event to be completed")
	}
}

func TestSyncApplicationsResolverFailureRecordsFailedEvents(t *testing.T) {
	setupTestDB(t)
	setupSyncQueue(t)
	repo := seedRepo(t)
	agent := seedAgent(t)
	app := seedApp(t, repo.Id, agent.Id, "services: {}\n")

	provider := &mockProvider{latestCommitErr: errors.New("connection refused")}
	nop := zerolog.Nop()
	SyncApplications(t.Context(), &repo, provider, []models.Application{app},
		LatestCommit(provider, &repo),
		SyncOrigin{Source: models.ApplicationEventSourceManual}, &nop)

	events := loadAppEvents(t, app.Id)
	if len(events) != 1 {
		t.Fatalf("expected 1 failed event, got %d", len(events))
	}
	if events[0].Status != models.ApplicationEventFailed || events[0].ErrorMessage == nil {
		t.Fatalf("expected failed event with error message, got %+v", events[0])
	}
}

func TestSyncApplicationsCanceledResolverStillRecordsFailedEvent(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	agent := seedAgent(t)
	app := seedApp(t, repo.Id, agent.Id, "services: {}\n")

	ctx, cancel := context.WithCancel(t.Context())
	resolve := func(context.Context, string) (string, string, error) {
		cancel()
		return "", "", errors.New("request canceled")
	}
	nop := zerolog.Nop()
	SyncApplications(ctx, &repo, &mockProvider{}, []models.Application{app}, resolve,
		SyncOrigin{Source: models.ApplicationEventSourceManual}, &nop)

	events := loadAppEvents(t, app.Id)
	if len(events) != 1 || events[0].Status != models.ApplicationEventFailed || events[0].CompletedAt == nil {
		t.Fatalf("expected completed failed event, got %+v", events)
	}
}

func TestSyncApplicationsMissingQueueRecordsFailedEvents(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	agent := seedAgent(t)
	app := seedApp(t, repo.Id, agent.Id, "services: {}\n")

	prev := DefaultQueue
	DefaultQueue = nil
	t.Cleanup(func() { DefaultQueue = prev })

	provider := &mockProvider{}
	nop := zerolog.Nop()
	SyncApplications(t.Context(), &repo, provider, []models.Application{app},
		StaticCommit("new-sha", "new commit"),
		SyncOrigin{Source: models.ApplicationEventSourceRepositoryWebhook}, &nop)

	events := loadAppEvents(t, app.Id)
	if len(events) != 1 {
		t.Fatalf("expected 1 failed event, got %d", len(events))
	}
	if events[0].Status != models.ApplicationEventFailed {
		t.Fatalf("expected failed event, got %+v", events[0])
	}
}

func TestQueueEnqueueFullQueueRecordsFailedEvent(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	agent := seedAgent(t)

	nop := zerolog.Nop()
	q := NewQueue(&nop)

	capacity := cap(q.jobs)
	apps := make([]models.Application, capacity+1)
	for i := range apps {
		apps[i] = seedApp(t, repo.Id, agent.Id, "services: {}\n")
	}

	provider := &mockProvider{}
	q.Enqueue(&repo, provider, apps, "new-sha", "new commit", SyncOrigin{Source: models.ApplicationEventSourceRepositoryWebhook})

	dropped := apps[capacity]
	events := loadAppEvents(t, dropped.Id)
	if len(events) != 1 {
		t.Fatalf("expected 1 failed event for dropped job, got %d", len(events))
	}
	if events[0].Status != models.ApplicationEventFailed {
		t.Fatalf("expected failed event, got %+v", events[0])
	}
	for _, queued := range apps[:capacity] {
		if got := loadAppEvents(t, queued.Id); len(got) != 0 {
			t.Fatalf("expected no events for queued app before processing, got %d", len(got))
		}
	}
}

var _ repositories.Provider = (*mockProvider)(nil)
