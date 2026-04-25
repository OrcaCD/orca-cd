package applications

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/repositories"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// mockProvider is a test double for repositories.Provider.
type mockProvider struct {
	latestCommit      repositories.CommitInfo
	latestCommitErr   error
	fileContent       string
	fileContentErr    error
	onGetFileContent  func()
	onGetLatestCommit func()
}

func (m *mockProvider) ParseURL(url string) (string, string, error)                  { return "", "", nil }
func (m *mockProvider) SupportedAuthMethods() []models.RepositoryAuthMethod          { return nil }
func (m *mockProvider) TestConnection(_ context.Context, _ *models.Repository) error { return nil }
func (m *mockProvider) ListBranches(_ context.Context, _ *models.Repository) ([]string, error) {
	return nil, nil
}
func (m *mockProvider) ListTree(_ context.Context, _ *models.Repository, _ string) ([]repositories.TreeEntry, error) {
	return nil, nil
}
func (m *mockProvider) GetFileContent(_ context.Context, _ *models.Repository, _ string, _ string) (string, error) {
	if m.onGetFileContent != nil {
		m.onGetFileContent()
	}
	return m.fileContent, m.fileContentErr
}
func (m *mockProvider) GetLatestCommit(_ context.Context, _ *models.Repository, _ string) (repositories.CommitInfo, error) {
	if m.onGetLatestCommit != nil {
		m.onGetLatestCommit()
	}
	return m.latestCommit, m.latestCommitErr
}

func setupTestDB(t *testing.T) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	testDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: gormlogger.New(
			log.New(os.Stderr, "\n", log.LstdFlags),
			gormlogger.Config{
				SlowThreshold:             200 * time.Millisecond,
				LogLevel:                  gormlogger.Warn,
				IgnoreRecordNotFoundError: true,
			},
		),
	})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	if err := testDB.AutoMigrate(&models.Agent{}, &models.Repository{}, &models.Application{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	sqlDB, err := testDB.DB()
	if err != nil {
		t.Fatalf("failed to get sql db: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
		db.DB = nil
	})

	db.DB = testDB

	if err := crypto.Init("test-secret-that-is-long-enough-32chars"); err != nil {
		t.Fatalf("failed to init crypto: %v", err)
	}
}

func seedRepo(t *testing.T) models.Repository {
	t.Helper()
	repo := models.Repository{
		Name:       "owner/repo",
		Url:        "https://github.com/owner/repo",
		Provider:   models.GitHub,
		AuthMethod: models.AuthMethodNone,
		SyncType:   models.SyncTypeManual,
		SyncStatus: models.SyncStatusUnknown,
		CreatedBy:  "user-1",
	}
	if err := db.DB.Select("*").Create(&repo).Error; err != nil {
		t.Fatalf("failed to seed repo: %v", err)
	}
	return repo
}

func seedAgent(t *testing.T) models.Agent {
	t.Helper()
	agent := models.Agent{
		Name:   crypto.EncryptedString("test-agent"),
		KeyId:  crypto.EncryptedString("test-key"),
		Status: models.AgentStatusOnline,
	}
	if err := db.DB.Select("*").Create(&agent).Error; err != nil {
		t.Fatalf("failed to seed agent: %v", err)
	}
	return agent
}

func seedApp(t *testing.T, repoId, agentId, composeFile string) models.Application {
	t.Helper()
	app := models.Application{
		Name:          crypto.EncryptedString("test-app"),
		RepositoryId:  repoId,
		AgentId:       agentId,
		SyncStatus:    models.UnknownSync,
		HealthStatus:  models.UnknownHealth,
		Branch:        "main",
		Commit:        "abc123",
		CommitMessage: "initial commit",
		Path:          "deploy.yml",
		ComposeFile:   crypto.EncryptedString(composeFile),
	}
	if err := db.DB.Select("*").Create(&app).Error; err != nil {
		t.Fatalf("failed to seed application: %v", err)
	}
	return app
}

// --- GetMatchingApplications ---

func TestGetMatchingApplications_ReturnsMatchingApps(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	agent := seedAgent(t)

	app := seedApp(t, repo.Id, agent.Id, "compose: v1")

	apps, err := GetMatchingApplications(t.Context(), &repo, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(apps))
	}
	if apps[0].Id != app.Id {
		t.Errorf("expected app id %q, got %q", app.Id, apps[0].Id)
	}
}

func TestGetMatchingApplications_EmptyWhenBranchMismatch(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	agent := seedAgent(t)
	seedApp(t, repo.Id, agent.Id, "compose: v1")

	apps, err := GetMatchingApplications(t.Context(), &repo, "develop")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(apps) != 0 {
		t.Errorf("expected 0 apps, got %d", len(apps))
	}
}

func TestGetMatchingApplications_EmptyWhenRepoMismatch(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	agent := seedAgent(t)
	seedApp(t, repo.Id, agent.Id, "compose: v1")

	otherRepo := models.Repository{
		Name:       "owner/other",
		Url:        "https://github.com/owner/other",
		Provider:   models.GitHub,
		AuthMethod: models.AuthMethodNone,
		SyncType:   models.SyncTypeManual,
		SyncStatus: models.SyncStatusUnknown,
		CreatedBy:  "user-1",
	}
	if err := db.DB.Select("*").Create(&otherRepo).Error; err != nil {
		t.Fatalf("failed to seed other repo: %v", err)
	}

	apps, err := GetMatchingApplications(t.Context(), &otherRepo, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(apps) != 0 {
		t.Errorf("expected 0 apps, got %d", len(apps))
	}
}

func TestGetMatchingApplications_ReturnsMultiple(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	agent := seedAgent(t)
	seedApp(t, repo.Id, agent.Id, "compose: v1")
	seedApp(t, repo.Id, agent.Id, "compose: v2")

	apps, err := GetMatchingApplications(t.Context(), &repo, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(apps) != 2 {
		t.Errorf("expected 2 apps, got %d", len(apps))
	}
}

// --- processSyncJob ---

func TestProcessSyncJob_NoComposeChange_SetsStatusSynced(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	agent := seedAgent(t)
	const compose = "services:\n  app:\n    image: myimage:1.0\n"
	app := seedApp(t, repo.Id, agent.Id, compose)

	provider := &mockProvider{fileContent: compose}
	nop := zerolog.Nop()

	processSyncJob(t.Context(), syncJob{
		Application:        app,
		Repository:         repo,
		RepositoryProvider: provider,
		Commit:             "newsha",
		CommitMessage:      "new commit",
	}, &nop)

	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load application: %v", err)
	}
	if updated.SyncStatus != models.Synced {
		t.Errorf("expected SyncStatus %q, got %q", models.Synced, updated.SyncStatus)
	}
	if updated.Commit != "newsha" {
		t.Errorf("expected Commit %q, got %q", "newsha", updated.Commit)
	}
	if updated.CommitMessage != "new commit" {
		t.Errorf("expected CommitMessage %q, got %q", "new commit", updated.CommitMessage)
	}
	if updated.LastSyncedAt == nil {
		t.Error("expected LastSyncedAt to be set")
	}
	if updated.ComposeFile.String() != compose {
		t.Errorf("expected ComposeFile unchanged, got %q", updated.ComposeFile.String())
	}
}

func TestProcessSyncJob_ComposeChanged_UpdatesComposeAndSetsStatusSynced(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	agent := seedAgent(t)
	const oldCompose = "services:\n  app:\n    image: myimage:1.0\n"
	const newCompose = "services:\n  app:\n    image: myimage:2.0\n"
	app := seedApp(t, repo.Id, agent.Id, oldCompose)

	provider := &mockProvider{fileContent: newCompose}
	nop := zerolog.Nop()

	processSyncJob(t.Context(), syncJob{
		Application:        app,
		Repository:         repo,
		RepositoryProvider: provider,
		Commit:             "newsha",
		CommitMessage:      "bump version",
	}, &nop)

	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load application: %v", err)
	}
	if updated.SyncStatus != models.Synced {
		t.Errorf("expected SyncStatus %q, got %q", models.Synced, updated.SyncStatus)
	}
	if updated.Commit != "newsha" {
		t.Errorf("expected Commit %q, got %q", "newsha", updated.Commit)
	}
	if updated.CommitMessage != "bump version" {
		t.Errorf("expected CommitMessage %q, got %q", "bump version", updated.CommitMessage)
	}
	if updated.LastSyncedAt == nil {
		t.Error("expected LastSyncedAt to be set")
	}
	if updated.ComposeFile.String() != newCompose {
		t.Errorf("expected ComposeFile %q, got %q", newCompose, updated.ComposeFile.String())
	}
}

func TestProcessSyncJob_GetFileContentError_SetsStatusOutOfSync(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	agent := seedAgent(t)
	app := seedApp(t, repo.Id, agent.Id, "compose: v1")

	provider := &mockProvider{
		latestCommit:   repositories.CommitInfo{Hash: "sha", Message: "msg"},
		fileContentErr: errors.New("file not found"),
	}
	nop := zerolog.Nop()

	processSyncJob(t.Context(), syncJob{
		Application:        app,
		Repository:         repo,
		RepositoryProvider: provider,
		Commit:             "sha",
	}, &nop)

	got, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load application: %v", err)
	}
	if got.SyncStatus != models.OutOfSync {
		t.Errorf("expected SyncStatus %q after file fetch error, got %q", models.OutOfSync, got.SyncStatus)
	}
	if got.LastSyncedAt != nil {
		t.Error("expected LastSyncedAt to remain nil")
	}
}

// --- Queue ---

func TestNewQueue_HasCorrectProperties(t *testing.T) {
	nop := zerolog.Nop()
	q := NewQueue(&nop)
	if q == nil {
		t.Fatal("expected non-nil queue")
	}
	if q.workers != defaultWorkerCount {
		t.Errorf("expected %d workers, got %d", defaultWorkerCount, q.workers)
	}
	if q.jobs == nil {
		t.Error("expected non-nil jobs channel")
	}
}

func TestProcessSyncJob_SetsSyncStatusToSyncing(t *testing.T) {
	// processSyncJob is responsible for the Syncing transition; Enqueue no longer touches the DB.
	setupTestDB(t)
	repo := seedRepo(t)
	agent := seedAgent(t)
	app := seedApp(t, repo.Id, agent.Id, "compose: v1")

	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	provider := &mockProvider{
		onGetFileContent: func() {
			close(started)
			<-release
		},
		fileContentErr: errors.New("stop here"),
	}
	nop := zerolog.Nop()

	go func() {
		processSyncJob(t.Context(), syncJob{
			Application:        app,
			Repository:         repo,
			RepositoryProvider: provider,
			Commit:             "sha",
		}, &nop)
		close(done)
	}()

	<-started // GetFileContent was called, meaning Syncing was already written

	got, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load application: %v", err)
	}
	if got.SyncStatus != models.Syncing {
		t.Errorf("expected SyncStatus %q at start of processing, got %q", models.Syncing, got.SyncStatus)
	}

	close(release)
	<-done
}

func TestQueue_Enqueue_JobAddedToChannel(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	agent := seedAgent(t)
	app := seedApp(t, repo.Id, agent.Id, "compose: v1")

	nop := zerolog.Nop()
	q := NewQueue(&nop)

	provider := &mockProvider{}
	q.Enqueue(&repo, provider, []models.Application{app}, "abc123", "")

	if len(q.jobs) != 1 {
		t.Errorf("expected 1 job in channel, got %d", len(q.jobs))
	}
}

func TestQueue_Enqueue_EmptyCommit_EnqueuesEmptyCommit(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	agent := seedAgent(t)
	app := seedApp(t, repo.Id, agent.Id, "compose: v1")

	nop := zerolog.Nop()
	q := NewQueue(&nop)

	provider := &mockProvider{}
	q.Enqueue(&repo, provider, []models.Application{app}, "", "")

	if len(q.jobs) != 1 {
		t.Fatalf("expected 1 job in channel, got %d", len(q.jobs))
	}
	job := <-q.jobs
	if job.Commit != "" {
		t.Errorf("expected empty commit %q, got %q", "", job.Commit)
	}
}

func TestQueue_Enqueue_EmptyApps_NoOp(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)

	nop := zerolog.Nop()
	q := NewQueue(&nop)

	provider := &mockProvider{}
	q.Enqueue(&repo, provider, []models.Application{}, "abc123", "")

	if len(q.jobs) != 0 {
		t.Errorf("expected 0 jobs in channel, got %d", len(q.jobs))
	}
}

func TestQueue_Enqueue_FullQueue_DropsJob(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	agent := seedAgent(t)

	nop := zerolog.Nop()
	q := NewQueue(&nop)

	provider := &mockProvider{}

	capacity := cap(q.jobs)
	apps := make([]models.Application, capacity+1)
	for i := range apps {
		apps[i] = seedApp(t, repo.Id, agent.Id, "compose: v1")
	}

	q.Enqueue(&repo, provider, apps, "abc123", "")

	if len(q.jobs) != capacity {
		t.Errorf("expected queue at capacity (%d), got %d", capacity, len(q.jobs))
	}

	// The one dropped app must not have had its status touched — it was never queued.
	droppedApp := apps[capacity]
	got, err := gorm.G[models.Application](db.DB).Where("id = ?", droppedApp.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load dropped application: %v", err)
	}
	if got.SyncStatus != models.UnknownSync {
		t.Errorf("expected dropped app SyncStatus %q, got %q", models.UnknownSync, got.SyncStatus)
	}
}

func TestQueue_Start_ProcessesJobs(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	agent := seedAgent(t)
	const compose = "services:\n  app:\n    image: myimage:1.0\n"
	app := seedApp(t, repo.Id, agent.Id, compose)

	nop := zerolog.Nop()
	q := NewQueue(&nop)
	q.Start()

	provider := &mockProvider{
		latestCommit: repositories.CommitInfo{Hash: "sha", Message: "msg"},
		fileContent:  compose,
	}
	q.Enqueue(&repo, provider, []models.Application{app}, "sha", "")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(t.Context())
		if err == nil && updated.SyncStatus == models.Synced {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("timed out waiting for job to be processed")
}
