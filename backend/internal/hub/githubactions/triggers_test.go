package githubactions

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestBuildResult(t *testing.T) {
	now := time.Now()
	trigger := models.GitHubActionsTrigger{
		CreatedAt: now,
		SyncRepo:  true, PullImages: true,
		ApplicationIds: []string{"app-a", "app-b"},
	}
	event := func(appID string, ty models.ApplicationEventType, status models.ApplicationEventStatus) models.ApplicationEvent {
		return models.ApplicationEvent{ApplicationId: appID, Type: ty, Status: status}
	}
	skipsForApp := func(appID string) []models.GitHubActionsTriggerSkip {
		return []models.GitHubActionsTriggerSkip{
			{ApplicationId: appID, Type: models.ApplicationEventCommitSync, Reason: "auto-sync paused"},
			{ApplicationId: appID, Type: models.ApplicationEventImageUpdate, Reason: "images pinned"},
		}
	}
	noChange := event("app-a", models.ApplicationEventCommitSync, models.ApplicationEventNoChange)
	updated := event("app-a", models.ApplicationEventImageUpdate, models.ApplicationEventSucceeded)
	failedSync := event("app-a", models.ApplicationEventCommitSync, models.ApplicationEventFailed)
	rollbackRunning := event("app-a", models.ApplicationEventRollback, models.ApplicationEventRunning)
	rollbackDone := event("app-a", models.ApplicationEventRollback, models.ApplicationEventSucceeded)

	tests := []struct {
		name          string
		events        []models.ApplicationEvent
		skips         []models.GitHubActionsTriggerSkip
		wantOverall   string
		wantFirstApp  string
		wantSecondApp string
	}{
		{"waiting for image update", []models.ApplicationEvent{noChange}, skipsForApp("app-b"), running, running, skipped},
		{"success with skips", []models.ApplicationEvent{noChange, updated}, skipsForApp("app-b"), succeeded, succeeded, skipped},
		{"waiting for rollback", []models.ApplicationEvent{failedSync, updated, rollbackRunning}, skipsForApp("app-b"), running, running, skipped},
		{"rollback makes result fail", []models.ApplicationEvent{failedSync, updated, rollbackDone}, skipsForApp("app-b"), failed, failed, skipped},
		{"another app still running", []models.ApplicationEvent{failedSync, updated}, nil, running, failed, running},
		{"all skipped", nil, append(skipsForApp("app-a"), skipsForApp("app-b")...), succeeded, skipped, skipped},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildResult(&trigger, tt.events, tt.skips, map[string]string{"app-a": "web", "app-b": "worker"}, now)
			if got.Status != tt.wantOverall {
				t.Errorf("overall status = %q, want %q", got.Status, tt.wantOverall)
			}
			if len(got.Applications) != 2 {
				t.Fatalf("expected 2 applications, got %+v", got.Applications)
			}
			if got.Applications[0].Status != tt.wantFirstApp || got.Applications[1].Status != tt.wantSecondApp {
				t.Errorf("unexpected application statuses: %+v", got.Applications)
			}
		})
	}
}

func TestBuildResultMissingOperations(t *testing.T) {
	start := time.Now()
	trigger := models.GitHubActionsTrigger{
		CreatedAt: start, ApplicationIds: []string{"app"}, SyncRepo: true, PullImages: true,
	}
	apps := map[string]string{"app": "web"}
	syncEvent := models.ApplicationEvent{
		ApplicationId: "app", Type: models.ApplicationEventCommitSync, Status: models.ApplicationEventSucceeded,
	}
	deadline := start.Add(operationRecordTimeout)
	pending := buildResult(&trigger, []models.ApplicationEvent{syncEvent}, nil, apps, deadline.Add(-time.Second))
	if pending.Status != running {
		t.Fatalf("before the deadline = %q, want running", pending.Status)
	}
	result := buildResult(&trigger, []models.ApplicationEvent{syncEvent}, nil, apps, deadline)
	if result.Status != failed || len(result.Applications[0].Events) != 2 {
		t.Fatalf("after the deadline, expected a failed pull alongside the completed sync: %+v", result)
	}
	pull := result.Applications[0].Events[1]
	if pull.Type != models.ApplicationEventImageUpdate || pull.Status != failed || pull.Error == nil {
		t.Errorf("unexpected pull result: %+v", pull)
	}
	syncEvent.Status = models.ApplicationEventRunning
	result = buildResult(&trigger, []models.ApplicationEvent{syncEvent}, nil, apps, deadline)
	if result.Status != running {
		t.Errorf("an operation already running must continue to run after the deadline, got %q", result.Status)
	}

	result = buildResult(&trigger, nil, nil, nil, start)
	if result.Status != failed || len(result.Applications[0].Events) != 2 || result.Applications[0].Name != "app" {
		t.Fatalf("deleted application should fail immediately: %+v", result)
	}
}

func TestGetResultAndPruneExpiredTriggers(t *testing.T) {
	testDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "triggers.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	previous := db.DB
	db.DB = testDB
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
		db.DB = previous
	})
	if err := crypto.Init("test-secret-that-is-long-enough-32chars"); err != nil {
		t.Fatalf("init crypto: %v", err)
	}
	t.Cleanup(func() { crypto.SetDefault(nil) })
	if err := testDB.AutoMigrate(&models.GitHubActionsTrigger{}, &models.GitHubActionsTriggerSkip{}, &models.ApplicationEvent{}); err != nil {
		t.Fatalf("migrate triggers: %v", err)
	}

	app := models.Application{Name: "web"}
	if err := gorm.G[models.Application](testDB).Create(t.Context(), &app); err != nil {
		t.Fatalf("create application: %v", err)
	}
	now := time.Now()
	for _, id := range []string{"expired", "current"} {
		trigger := models.GitHubActionsTrigger{
			Id: id, RepositoryId: "repo", Repository: "owner/repo", RunId: "42",
			SyncRepo: true, PullImages: true, ApplicationIds: []string{app.Id},
		}
		if err := gorm.G[models.GitHubActionsTrigger](testDB).Create(t.Context(), &trigger); err != nil {
			t.Fatalf("create trigger: %v", err)
		}
	}
	event := models.ApplicationEvent{
		ApplicationId: app.Id, TriggerId: new("current"), Type: models.ApplicationEventCommitSync,
		Source: models.ApplicationEventSourceGitHubActions, Status: models.ApplicationEventNoChange,
	}
	if err := gorm.G[models.ApplicationEvent](testDB).Create(t.Context(), &event); err != nil {
		t.Fatalf("record sync event: %v", err)
	}
	skip := models.GitHubActionsTriggerSkip{
		TriggerId: "current", ApplicationId: app.Id, Type: models.ApplicationEventImageUpdate, Reason: "images pinned",
	}
	if err := gorm.G[models.GitHubActionsTriggerSkip](testDB).Create(t.Context(), &skip); err != nil {
		t.Fatalf("record skipped pull: %v", err)
	}
	result, err := GetResult(t.Context(), "current", "Owner/Repo", "42")
	if err != nil || result.Status != succeeded || len(result.Applications) != 1 {
		t.Fatalf("expected completed trigger, got %+v, %v", result, err)
	}
	resultApp := result.Applications[0]
	if resultApp.Name != "web" || len(resultApp.Events) != 2 || resultApp.Events[1].Status != skipped ||
		resultApp.Events[1].Error == nil || *resultApp.Events[1].Error != skip.Reason {
		t.Errorf("expected application name and reason for the skipped pull, got %+v", resultApp)
	}
	if _, err := GetResult(t.Context(), "current", "owner/repo", "43"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("other run should not see trigger, got %v", err)
	}

	if _, err := gorm.G[models.GitHubActionsTrigger](testDB).Where("id = ?", "expired").
		Update(t.Context(), "created_at", now.Add(-triggerRetention-time.Minute)); err != nil {
		t.Fatalf("age trigger: %v", err)
	}
	// Startup must prune before the first hourly tick.
	log := zerolog.Nop()
	stop := StartPruner(&log)
	defer stop()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		remaining, err := gorm.G[models.GitHubActionsTrigger](testDB).Find(t.Context())
		if err != nil {
			t.Fatalf("load triggers: %v", err)
		}
		if len(remaining) == 1 && remaining[0].Id == "current" {
			break
		}
		select {
		case <-deadline.C:
			t.Fatalf("expected only the current trigger, got %+v", remaining)
		case <-ticker.C:
		}
	}
}
