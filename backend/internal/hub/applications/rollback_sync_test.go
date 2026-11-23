package applications

import (
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	application_deployer "github.com/OrcaCD/orca-cd/internal/hub/deployer"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

func TestProcessSyncJob_RolledBackApplication(t *testing.T) {
	const compose = "services:\n  app:\n    image: myimage:1.0\n"
	tests := []struct {
		name       string
		paused     bool
		source     models.ApplicationEventSource
		commit     string
		wantDeploy bool
	}{
		{name: "paused skips a new commit", paused: true, source: models.ApplicationEventSourceRepositoryWebhook, commit: "newsha"},
		{name: "paused skips GitHub Actions", paused: true, source: models.ApplicationEventSourceGitHubActions, commit: "newsha"},
		{name: "skips the rolled back commit", source: models.ApplicationEventSourceRepositoryWebhook, commit: "abc123"},
		{name: "deploys a new commit with an unchanged compose file", source: models.ApplicationEventSourceRepositoryPolling, commit: "newsha", wantDeploy: true},
		{name: "manual sync deploys while paused", paused: true, source: models.ApplicationEventSourceManual, commit: "abc123", wantDeploy: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupTestDB(t)
			repo := seedRepo(t)
			agent := seedAgent(t)
			app := seedApp(t, repo.Id, agent.Id, compose)
			now := time.Now()
			app.RolledBackAt = &now
			app.AutoSyncPaused = tt.paused

			stub := &stubDeployer{}
			prev := application_deployer.DefaultApplicationDeployer
			application_deployer.DefaultApplicationDeployer = stub
			t.Cleanup(func() { application_deployer.DefaultApplicationDeployer = prev })
			nop := zerolog.Nop()

			processSyncJob(t.Context(), syncJob{
				Application:        app,
				Repository:         repo,
				RepositoryProvider: &mockProvider{fileContent: compose},
				Commit:             tt.commit,
				Origin:             SyncOrigin{Source: tt.source},
			}, &nop)

			if stub.called != tt.wantDeploy {
				t.Errorf("deployed = %v, want %v", stub.called, tt.wantDeploy)
			}
		})
	}
}

func TestTriggerImagePull_SkipsRolledBackApplication(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	agent := seedAgent(t)
	app := seedApp(t, repo.Id, agent.Id, "services: {}\n")
	now := time.Now()
	app.RolledBackAt = &now

	if !TriggerImagePull(&app, SyncOrigin{Source: models.ApplicationEventSourceImageWebhook}) {
		t.Error("expected a skipped pull to be reported as accepted")
	}
	events := loadAppEvents(t, app.Id)
	if len(events) != 0 {
		t.Errorf("expected no image update event, got %v", events)
	}
}

func TestGitHubActionsRecordsRolledBackOperationSkips(t *testing.T) {
	setupTestDB(t)
	if err := db.DB.AutoMigrate(&models.GitHubActionsTriggerSkip{}); err != nil {
		t.Fatalf("migrate trigger skips: %v", err)
	}
	repo := seedRepo(t)
	agent := seedAgent(t)
	app := seedApp(t, repo.Id, agent.Id, "services: {}\n")
	now := time.Now()
	app.RolledBackAt = &now
	app.AutoSyncPaused = true
	triggerID := "workflow-trigger"
	origin := SyncOrigin{Source: models.ApplicationEventSourceGitHubActions, TriggerID: &triggerID}
	nop := zerolog.Nop()

	processSyncJob(t.Context(), syncJob{Application: app, Origin: origin, Commit: "newsha"}, &nop)
	if !TriggerImagePull(&app, origin) {
		t.Fatal("skipped pull should be accepted")
	}

	skips, err := gorm.G[models.GitHubActionsTriggerSkip](db.DB).
		Where("trigger_id = ? AND application_id = ?", triggerID, app.Id).
		Order("type").Find(t.Context())
	if err != nil {
		t.Fatalf("load skipped operations: %v", err)
	}
	if len(skips) != 2 || skips[0].Type != models.ApplicationEventCommitSync ||
		skips[1].Type != models.ApplicationEventImageUpdate || skips[0].Reason == "" || skips[1].Reason == "" {
		t.Errorf("expected skip reasons for sync and image pull, got %+v", skips)
	}
	if events := loadAppEvents(t, app.Id); len(events) != 0 {
		t.Errorf("expected no history events for skipped operations, got %+v", events)
	}
}
