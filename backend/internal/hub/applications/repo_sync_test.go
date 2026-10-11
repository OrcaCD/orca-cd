package applications

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/repositories"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

const testSyncProvider models.RepositoryProvider = "test_sync_provider"

func TestStaticCommit(t *testing.T) {
	resolve := StaticCommit("abc123", "initial commit")

	// Same values returned regardless of branch argument.
	for _, branch := range []string{"main", "dev", ""} {
		hash, msg, err := resolve(context.Background(), branch)
		if err != nil {
			t.Fatalf("branch %q: unexpected error: %v", branch, err)
		}
		if hash != "abc123" {
			t.Errorf("branch %q: hash = %q, want %q", branch, hash, "abc123")
		}
		if msg != "initial commit" {
			t.Errorf("branch %q: message = %q, want %q", branch, msg, "initial commit")
		}
	}
}

func setupSyncQueue(t *testing.T) *Queue {
	t.Helper()
	nop := zerolog.Nop()
	q := NewQueue(&nop)
	DefaultQueue = q
	t.Cleanup(func() { DefaultQueue = nil })
	return q
}

func TestSyncRepository_UnsupportedProvider(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	repo.Provider = "definitely_not_a_real_provider"

	nop := zerolog.Nop()
	SyncRepository(t.Context(), &repo, SyncOrigin{Source: models.ApplicationEventSourceManual}, &nop)

	got, err := gorm.G[models.Repository](db.DB).Where("id = ?", repo.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load repository: %v", err)
	}
	if got.SyncStatus != models.SyncStatusFailed {
		t.Errorf("expected SyncStatus %q, got %q", models.SyncStatusFailed, got.SyncStatus)
	}
	if got.LastSyncError == nil {
		t.Error("expected LastSyncError to be set")
	}
}

func TestSyncRepository_NoApplications_MarksSuccess(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	repo.Provider = testSyncProvider
	repositories.Register(testSyncProvider, &mockProvider{})

	nop := zerolog.Nop()
	SyncRepository(t.Context(), &repo, SyncOrigin{Source: models.ApplicationEventSourceManual}, &nop)

	got, err := gorm.G[models.Repository](db.DB).Where("id = ?", repo.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load repository: %v", err)
	}
	if got.SyncStatus != models.SyncStatusSuccess {
		t.Errorf("expected SyncStatus %q, got %q", models.SyncStatusSuccess, got.SyncStatus)
	}
	if got.LastSyncedAt == nil {
		t.Error("expected LastSyncedAt to be set")
	}
}

func TestSyncRepository_AppWithEmptyBranch_Skipped(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	repo.Provider = testSyncProvider
	repositories.Register(testSyncProvider, &mockProvider{
		latestCommitErr: errors.New("should not be called"),
	})
	agent := seedAgent(t)
	app := models.Application{
		Name:          crypto.EncryptedString("test-app"),
		RepositoryId:  repo.Id,
		AgentId:       agent.Id,
		SyncStatus:    models.UnknownSync,
		HealthStatus:  models.UnknownHealth,
		Branch:        "",
		Commit:        "abc123",
		CommitMessage: "initial",
		Path:          "deploy.yml",
		ComposeFile:   crypto.EncryptedString("compose: v1"),
	}
	if err := db.DB.Select("*").Create(&app).Error; err != nil {
		t.Fatalf("failed to create app with empty branch: %v", err)
	}

	nop := zerolog.Nop()
	SyncRepository(t.Context(), &repo, SyncOrigin{Source: models.ApplicationEventSourceManual}, &nop)

	got, err := gorm.G[models.Repository](db.DB).Where("id = ?", repo.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load repository: %v", err)
	}
	if got.SyncStatus != models.SyncStatusSuccess {
		t.Errorf("expected SyncStatus %q for repo with only empty-branch apps, got %q", models.SyncStatusSuccess, got.SyncStatus)
	}
}

func TestSyncRepository_GetLatestCommitError_MarksFailed(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	repo.Provider = testSyncProvider
	agent := seedAgent(t)
	seedApp(t, repo.Id, agent.Id, "compose: v1")
	repositories.Register(testSyncProvider, &mockProvider{
		latestCommitErr: errors.New("connection refused"),
	})

	nop := zerolog.Nop()
	SyncRepository(t.Context(), &repo, SyncOrigin{Source: models.ApplicationEventSourceManual}, &nop)

	got, err := gorm.G[models.Repository](db.DB).Where("id = ?", repo.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load repository: %v", err)
	}
	if got.SyncStatus != models.SyncStatusFailed {
		t.Errorf("expected SyncStatus %q, got %q", models.SyncStatusFailed, got.SyncStatus)
	}
	if got.LastSyncError == nil {
		t.Fatal("expected LastSyncError to be set")
	}
	if !strings.Contains(*got.LastSyncError, "main") {
		t.Errorf("expected error to mention branch name, got: %q", *got.LastSyncError)
	}
}

func TestSyncRepository_Success_EnqueuesJob(t *testing.T) {
	setupTestDB(t)
	q := setupSyncQueue(t)
	repo := seedRepo(t)
	repo.Provider = testSyncProvider
	agent := seedAgent(t)
	seedApp(t, repo.Id, agent.Id, "compose: v1")
	repositories.Register(testSyncProvider, &mockProvider{
		latestCommit: repositories.CommitInfo{Hash: "abc123", Message: "feat: new"},
	})

	nop := zerolog.Nop()
	SyncRepository(t.Context(), &repo, SyncOrigin{Source: models.ApplicationEventSourceManual}, &nop)

	if len(q.jobs) != 1 {
		t.Fatalf("expected 1 job in queue, got %d", len(q.jobs))
	}
	job := <-q.jobs
	if job.Commit != "abc123" {
		t.Errorf("expected commit %q, got %q", "abc123", job.Commit)
	}
	if job.CommitMessage != "feat: new" {
		t.Errorf("expected commit message %q, got %q", "feat: new", job.CommitMessage)
	}

	got, err := gorm.G[models.Repository](db.DB).Where("id = ?", repo.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load repository: %v", err)
	}
	if got.SyncStatus != models.SyncStatusSuccess {
		t.Errorf("expected SyncStatus %q, got %q", models.SyncStatusSuccess, got.SyncStatus)
	}
	if got.LastSyncedAt == nil {
		t.Error("expected LastSyncedAt to be set")
	}
}

func TestSyncRepository_MultipleAppsOnSameBranch_EnqueuesBoth(t *testing.T) {
	setupTestDB(t)
	q := setupSyncQueue(t)
	repo := seedRepo(t)
	repo.Provider = testSyncProvider
	agent := seedAgent(t)
	seedApp(t, repo.Id, agent.Id, "compose: v1")
	seedApp(t, repo.Id, agent.Id, "compose: v2")
	repositories.Register(testSyncProvider, &mockProvider{
		latestCommit: repositories.CommitInfo{Hash: "sha", Message: "msg"},
	})

	nop := zerolog.Nop()
	SyncRepository(t.Context(), &repo, SyncOrigin{Source: models.ApplicationEventSourceManual}, &nop)

	if len(q.jobs) != 2 {
		t.Errorf("expected 2 jobs for 2 apps on same branch, got %d", len(q.jobs))
	}

	got, err := gorm.G[models.Repository](db.DB).Where("id = ?", repo.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load repository: %v", err)
	}
	if got.SyncStatus != models.SyncStatusSuccess {
		t.Errorf("expected SyncStatus %q, got %q", models.SyncStatusSuccess, got.SyncStatus)
	}
}

func TestSyncRepository_MarksSyncingBeforeCommitLookup(t *testing.T) {
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
	done := make(chan struct{})
	go func() {
		SyncRepository(t.Context(), &repo, SyncOrigin{Source: models.ApplicationEventSourceManual}, &nop)
		close(done)
	}()

	<-started // GetLatestCommit called → markRepositorySyncing already ran

	got, err := gorm.G[models.Repository](db.DB).Where("id = ?", repo.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load repository: %v", err)
	}
	if got.SyncStatus != models.SyncStatusSyncing {
		t.Errorf("expected SyncStatus %q during sync, got %q", models.SyncStatusSyncing, got.SyncStatus)
	}

	close(release)
	<-done
}

type repositoryNotification struct {
	repositoryId string
	event        models.NotificationEvent
	message      string
}

func captureRepositoryNotifications(t *testing.T) *[]repositoryNotification {
	t.Helper()
	var sent []repositoryNotification
	prev := sendForRepository
	sendForRepository = func(repositoryId string, event models.NotificationEvent, message string, _ *zerolog.Logger) {
		sent = append(sent, repositoryNotification{repositoryId, event, message})
	}
	t.Cleanup(func() { sendForRepository = prev })
	return &sent
}

func repositoryNotificationEvents(sent []repositoryNotification) []models.NotificationEvent {
	events := make([]models.NotificationEvent, 0, len(sent))
	for _, n := range sent {
		events = append(events, n.event)
	}
	return events
}

func TestRepositorySyncNotifications_OnlyOnTransitions(t *testing.T) {
	setupTestDB(t)
	sent := captureRepositoryNotifications(t)
	repo := seedRepo(t)
	repo.Provider = testSyncProvider
	agent := seedAgent(t)
	seedApp(t, repo.Id, agent.Id, "compose: v1")
	nop := zerolog.Nop()

	failing := &mockProvider{latestCommitErr: errors.New("connection refused")}
	repositories.Register(testSyncProvider, failing)
	// Every sync marks the repository as syncing first; the repeated failure
	// must still be recognized as "already failing".
	SyncRepository(t.Context(), &repo, SyncOrigin{Source: models.ApplicationEventSourceManual}, &nop)
	SyncRepository(t.Context(), &repo, SyncOrigin{Source: models.ApplicationEventSourceManual}, &nop)

	if got := repositoryNotificationEvents(*sent); len(got) != 1 || got[0] != models.NotificationEventRepositorySyncFailed {
		t.Fatalf("expected a single failed notification, got %v", got)
	}
	if n := (*sent)[0]; n.repositoryId != repo.Id || !strings.Contains(n.message, repo.Name) || !strings.Contains(n.message, "connection refused") {
		t.Fatalf("unexpected failed notification %+v", n)
	}

	now := time.Now()
	markRepositorySyncing(t.Context(), repo.Id, &nop)
	markRepositorySuccess(t.Context(), &repo, &now, &nop)
	markRepositorySyncing(t.Context(), repo.Id, &nop)
	markRepositorySuccess(t.Context(), &repo, &now, &nop)

	markRepositoryFailed(t.Context(), &repo, "boom", &nop)

	want := []models.NotificationEvent{
		models.NotificationEventRepositorySyncFailed,
		models.NotificationEventRepositorySyncRecovered,
		models.NotificationEventRepositorySyncFailed,
	}
	if got := repositoryNotificationEvents(*sent); !slices.Equal(got, want) {
		t.Fatalf("expected events %v, got %v", want, got)
	}
}

func TestRepositorySyncNotifications_FirstSuccessIsSilent(t *testing.T) {
	setupTestDB(t)
	sent := captureRepositoryNotifications(t)
	repo := seedRepo(t)
	nop := zerolog.Nop()

	now := time.Now()
	markRepositorySuccess(t.Context(), &repo, &now, &nop)

	if len(*sent) != 0 {
		t.Fatalf("expected no notification for a repository that never failed, got %v", *sent)
	}
}

func TestRepositorySyncNotifications_FailedUpdatesErrorWithoutRenotifying(t *testing.T) {
	setupTestDB(t)
	sent := captureRepositoryNotifications(t)
	repo := seedRepo(t)
	nop := zerolog.Nop()

	markRepositoryFailed(t.Context(), &repo, "first error", &nop)
	markRepositoryFailed(t.Context(), &repo, "second error", &nop)

	if len(*sent) != 1 {
		t.Fatalf("expected one notification, got %v", *sent)
	}
	got, err := gorm.G[models.Repository](db.DB).Where("id = ?", repo.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load repository: %v", err)
	}
	if got.LastSyncError == nil || *got.LastSyncError != "second error" {
		t.Fatalf("expected latest error to be stored, got %v", got.LastSyncError)
	}
}

func TestTruncateNotificationDetail(t *testing.T) {
	short := "short error"
	if got := truncateNotificationDetail(short); got != short {
		t.Errorf("expected short detail unchanged, got %q", got)
	}

	long := strings.Repeat("ä", maxNotificationDetailLength+10)
	got := truncateNotificationDetail(long)
	if want := strings.Repeat("ä", maxNotificationDetailLength) + "…"; got != want {
		t.Errorf("expected detail truncated to %d runes, got %d runes", maxNotificationDetailLength, len([]rune(got)))
	}
}
