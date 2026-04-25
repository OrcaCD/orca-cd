package applications

import (
	"errors"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/repositories"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

const testSyncProvider models.RepositoryProvider = "test_sync_provider"

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
	SyncRepository(t.Context(), &repo, &nop)

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
	SyncRepository(t.Context(), &repo, &nop)

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
	SyncRepository(t.Context(), &repo, &nop)

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
	SyncRepository(t.Context(), &repo, &nop)

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
	SyncRepository(t.Context(), &repo, &nop)

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
	SyncRepository(t.Context(), &repo, &nop)

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
		SyncRepository(t.Context(), &repo, &nop)
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
