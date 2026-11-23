package githubactions

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestBuildResult(t *testing.T) {
	trigger := models.GitHubActionsTrigger{
		SyncRepo: true, PullImages: true,
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
			got := buildResult(&trigger, tt.events, tt.skips, nil)
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
		Update(t.Context(), "created_at", now.Add(-triggerRetention)); err != nil {
		t.Fatalf("age trigger: %v", err)
	}
	if err := prune(t.Context(), now); err != nil {
		t.Fatalf("prune triggers: %v", err)
	}
	remaining, err := gorm.G[models.GitHubActionsTrigger](testDB).Find(t.Context())
	if err != nil || len(remaining) != 1 || remaining[0].Id != "current" {
		t.Fatalf("expected only the current trigger, got %+v, %v", remaining, err)
	}
}
