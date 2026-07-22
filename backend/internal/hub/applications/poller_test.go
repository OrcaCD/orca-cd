package applications

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/repositories"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

func TestIsDue(t *testing.T) {
	now := time.Now()
	interval := 60 * time.Second

	repoWithInterval := func(lastSyncedAt *time.Time) models.Repository {
		return models.Repository{
			PollingInterval: &interval,
			LastSyncedAt:    lastSyncedAt,
		}
	}

	t.Run("nil interval is never due", func(t *testing.T) {
		repo := models.Repository{PollingInterval: nil}
		if isDue(&repo, now) {
			t.Error("expected isDue=false for nil interval")
		}
	})

	t.Run("nil lastSyncedAt is always due", func(t *testing.T) {
		repo := repoWithInterval(nil)
		if !isDue(&repo, now) {
			t.Error("expected isDue=true when never synced")
		}
	})

	t.Run("just synced is not due", func(t *testing.T) {
		recent := now.Add(-10 * time.Second)
		repo := repoWithInterval(&recent)
		if isDue(&repo, now) {
			t.Error("expected isDue=false when synced 10s ago with 60s interval")
		}
	})

	t.Run("exactly at interval boundary is due", func(t *testing.T) {
		boundary := now.Add(-interval)
		repo := repoWithInterval(&boundary)
		if !isDue(&repo, now) {
			t.Error("expected isDue=true at exact interval boundary")
		}
	})

	t.Run("overdue is due", func(t *testing.T) {
		old := now.Add(-2 * interval)
		repo := repoWithInterval(&old)
		if !isDue(&repo, now) {
			t.Error("expected isDue=true when overdue")
		}
	})
}

func seedPollingRepo(t *testing.T, name string, lastSyncedAt *time.Time) models.Repository {
	t.Helper()
	interval := 60 * time.Second
	repo := models.Repository{
		Name:            "owner/" + name,
		Url:             "https://example.com/" + name,
		Provider:        testSyncProvider,
		AuthMethod:      models.AuthMethodNone,
		SyncType:        models.SyncTypePolling,
		SyncStatus:      models.SyncStatusUnknown,
		PollingInterval: &interval,
		LastSyncedAt:    lastSyncedAt,
		CreatedBy:       "user-1",
	}
	if err := db.DB.Select("*").Create(&repo).Error; err != nil {
		t.Fatalf("failed to seed polling repo %q: %v", name, err)
	}
	return repo
}

func TestNewPoller_HasCorrectProperties(t *testing.T) {
	nop := zerolog.Nop()
	p := NewPoller(&nop)
	if p == nil {
		t.Fatal("expected non-nil poller")
		return
	}
	if p.done == nil {
		t.Error("expected non-nil done channel")
	}
	if p.log == nil {
		t.Error("expected non-nil log")
	}
	if p.ctx == nil {
		t.Error("expected non-nil context")
	}
}

func TestPoller_Stop_IsIdempotent(t *testing.T) {
	nop := zerolog.Nop()
	p := NewPoller(&nop)
	p.Stop()
	p.Stop()
}

func TestPoller_TriggerSync_NilDB_IsNoOp(t *testing.T) {

	nop := zerolog.Nop()
	p := NewPoller(&nop)

	var repo models.Repository
	p.TriggerSync(&repo, SyncOrigin{Source: models.ApplicationEventSourceRepositoryPolling})
	p.Stop()
}

func TestPoller_TriggerSync_DeduplicatesConcurrentSync(t *testing.T) {
	setupTestDB(t)
	setupSyncQueue(t)

	repo := seedRepo(t)
	repo.Provider = testSyncProvider
	agent := seedAgent(t)
	seedApp(t, repo.Id, agent.Id, "compose: v1")

	var callCount atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	repositories.Register(testSyncProvider, &mockProvider{
		onGetLatestCommit: func() {
			if callCount.Add(1) == 1 {
				close(started)
				<-release
			}
		},
		latestCommit: repositories.CommitInfo{Hash: "sha", Message: "msg"},
	})

	nop := zerolog.Nop()
	p := NewPoller(&nop)

	p.TriggerSync(&repo, SyncOrigin{Source: models.ApplicationEventSourceRepositoryPolling})
	<-started // first sync is blocked in GetLatestCommit

	p.TriggerSync(&repo, SyncOrigin{Source: models.ApplicationEventSourceRepositoryPolling}) // deduplicated — syncing map has the repo ID
	p.TriggerSync(&repo, SyncOrigin{Source: models.ApplicationEventSourceRepositoryPolling}) // deduplicated

	close(release)
	p.Stop() // drains the one in-flight goroutine

	if n := callCount.Load(); n != 1 {
		t.Errorf("expected 1 GetLatestCommit call due to deduplication, got %d", n)
	}
}

func TestPoller_TriggerSync_ManualConflictRecordsFailedEvent(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	agent := seedAgent(t)
	apps := []models.Application{
		seedApp(t, repo.Id, agent.Id, "compose: v1"),
		seedApp(t, repo.Id, agent.Id, "compose: v2"),
	}

	nop := zerolog.Nop()
	p := NewPoller(&nop)
	t.Cleanup(p.Stop)
	p.syncing.Store(repo.Id, struct{}{})

	actorID, actorName := "user-1", "Alex"
	accepted := p.TriggerSync(&repo, SyncOrigin{
		Source:      models.ApplicationEventSourceManual,
		ActorUserID: &actorID,
		ActorName:   &actorName,
	})

	if accepted {
		t.Fatal("expected concurrent manual sync to be rejected")
	}
	for i := range apps {
		events := loadAppEvents(t, apps[i].Id)
		if len(events) != 1 {
			t.Fatalf("expected 1 failed event for application %q, got %d", apps[i].Id, len(events))
		}
		event := events[0]
		if event.Type != models.ApplicationEventCommitSync ||
			event.Source != models.ApplicationEventSourceManual ||
			event.Status != models.ApplicationEventFailed ||
			event.ErrorMessage == nil ||
			*event.ErrorMessage != repositorySyncInProgressMessage {
			t.Fatalf("unexpected event: %+v", event)
		}
		if event.ActorUserId == nil || *event.ActorUserId != actorID {
			t.Errorf("expected actor user id %q, got %v", actorID, event.ActorUserId)
		}
	}

	accepted = p.TriggerSync(&repo, SyncOrigin{Source: models.ApplicationEventSourceRepositoryPolling})
	if accepted {
		t.Fatal("expected concurrent scheduled sync to be rejected")
	}
	for i := range apps {
		if events := loadAppEvents(t, apps[i].Id); len(events) != 1 {
			t.Fatalf("expected scheduled conflict to stay silent for application %q, got %d events", apps[i].Id, len(events))
		}
	}
}

func TestPoller_TryLockRepositorySync_DeduplicatesAcrossCallers(t *testing.T) {
	nop := zerolog.Nop()
	p := NewPoller(&nop)
	t.Cleanup(p.Stop)

	release, ok := p.TryLockRepositorySync("repo-1")
	if !ok {
		t.Fatal("expected first lock attempt to succeed")
	}

	if _, ok := p.TryLockRepositorySync("repo-1"); ok {
		t.Error("expected second lock attempt for the same repository to be rejected while the first is held")
	}

	// A different repository must not be blocked by repo-1's lock.
	otherRelease, ok := p.TryLockRepositorySync("repo-2")
	if !ok {
		t.Fatal("expected lock attempt for a different repository to succeed")
	}
	otherRelease()

	release()

	if _, ok := p.TryLockRepositorySync("repo-1"); !ok {
		t.Error("expected lock attempt to succeed again after release")
	}
}

func TestPoller_TryLockRepositorySync_CoordinatesWithTriggerSync(t *testing.T) {
	setupTestDB(t)
	setupSyncQueue(t)

	repo := seedRepo(t)
	repo.Provider = testSyncProvider
	agent := seedAgent(t)
	seedApp(t, repo.Id, agent.Id, "compose: v1")

	started := make(chan struct{})
	release := make(chan struct{})
	repositories.Register(testSyncProvider, &mockProvider{
		onGetLatestCommit: func() {
			close(started)
			<-release
		},
		latestCommit: repositories.CommitInfo{Hash: "sha", Message: "msg"},
	})

	nop := zerolog.Nop()
	p := NewPoller(&nop)
	t.Cleanup(p.Stop)

	p.TriggerSync(&repo, SyncOrigin{Source: models.ApplicationEventSourceRepositoryPolling})
	<-started // TriggerSync's sync is in-flight, holding the repo's syncing slot

	if _, ok := p.TryLockRepositorySync(repo.Id); ok {
		t.Error("expected TryLockRepositorySync to be rejected while TriggerSync holds the same repository's slot")
	}

	close(release)
}

func TestPoller_Stop_WaitsForInFlightSyncs(t *testing.T) {
	setupTestDB(t)
	setupSyncQueue(t)

	repo := seedRepo(t)
	repo.Provider = testSyncProvider
	agent := seedAgent(t)
	seedApp(t, repo.Id, agent.Id, "compose: v1")

	started := make(chan struct{})
	release := make(chan struct{})
	repositories.Register(testSyncProvider, &mockProvider{
		onGetLatestCommit: func() {
			close(started)
			<-release
		},
		latestCommit: repositories.CommitInfo{Hash: "sha", Message: "msg"},
	})

	nop := zerolog.Nop()
	p := NewPoller(&nop)
	p.TriggerSync(&repo, SyncOrigin{Source: models.ApplicationEventSourceRepositoryPolling})
	<-started // sync is now in-flight

	stopped := make(chan struct{})
	go func() {
		p.Stop()
		close(stopped)
	}()

	select {
	case <-stopped:
		t.Error("Stop() returned before in-flight sync completed")
	case <-time.After(50 * time.Millisecond):
		// Stop() is waiting on wg, as expected
	}

	close(release)

	select {
	case <-stopped:
		// Good: Stop() returned after the sync was released
	case <-time.After(2 * time.Second):
		t.Error("Stop() did not return after in-flight sync completed")
	}
}

func TestPoller_PollRepositories_NilDB_IsNoOp(t *testing.T) {
	// Deliberately no setupTestDB — db.DB remains nil.
	nop := zerolog.Nop()
	p := NewPoller(&nop)
	p.pollRepositories() // nil-DB guard: must return immediately
	p.Stop()
}

func TestPoller_PollRepositories_OnlyTriggersPollingRepos(t *testing.T) {
	setupTestDB(t)
	setupSyncQueue(t)
	repositories.Register(testSyncProvider, &mockProvider{
		latestCommit: repositories.CommitInfo{Hash: "sha", Message: "msg"},
	})

	interval := 60 * time.Second
	past := time.Now().Add(-2 * interval)
	pollingRepo := seedPollingRepo(t, "poll-repo", &past)
	manualRepo := seedRepo(t) // SyncTypeManual — must never be touched

	nop := zerolog.Nop()
	p := NewPoller(&nop)
	defer p.Stop()

	p.pollRepositories()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err := gorm.G[models.Repository](db.DB).Where("id = ?", pollingRepo.Id).First(t.Context())
		if err == nil && got.SyncStatus != models.SyncStatusUnknown {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	pollingGot, err := gorm.G[models.Repository](db.DB).Where("id = ?", pollingRepo.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load polling repo: %v", err)
	}
	if pollingGot.SyncStatus == models.SyncStatusUnknown {
		t.Error("expected polling repo SyncStatus to change after sync")
	}

	manualGot, err := gorm.G[models.Repository](db.DB).Where("id = ?", manualRepo.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load manual repo: %v", err)
	}
	if manualGot.SyncStatus != models.SyncStatusUnknown {
		t.Errorf("expected manual repo SyncStatus to remain Unknown, got %q", manualGot.SyncStatus)
	}
}

func TestPoller_PollRepositories_SkipsNotDueRepos(t *testing.T) {
	setupTestDB(t)
	setupSyncQueue(t)
	repositories.Register(testSyncProvider, &mockProvider{
		latestCommit: repositories.CommitInfo{Hash: "sha", Message: "msg"},
	})

	interval := 60 * time.Second
	past := time.Now().Add(-2 * interval)       // overdue
	recent := time.Now().Add(-10 * time.Second) // not yet due

	dueRepo := seedPollingRepo(t, "due-repo", &past)
	notDueRepo := seedPollingRepo(t, "notdue-repo", &recent)

	nop := zerolog.Nop()
	p := NewPoller(&nop)
	defer p.Stop()

	p.pollRepositories()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err := gorm.G[models.Repository](db.DB).Where("id = ?", dueRepo.Id).First(t.Context())
		if err == nil && got.SyncStatus != models.SyncStatusUnknown {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	dueGot, err := gorm.G[models.Repository](db.DB).Where("id = ?", dueRepo.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load due repo: %v", err)
	}
	if dueGot.SyncStatus == models.SyncStatusUnknown {
		t.Error("expected due repo SyncStatus to change after sync")
	}

	notDueGot, err := gorm.G[models.Repository](db.DB).Where("id = ?", notDueRepo.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load not-due repo: %v", err)
	}
	if notDueGot.SyncStatus != models.SyncStatusUnknown {
		t.Errorf("expected not-due repo SyncStatus to remain Unknown, got %q", notDueGot.SyncStatus)
	}
}
