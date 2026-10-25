package applications

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/commitsigning"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/repositories"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

const testSigningProvider models.RepositoryProvider = "test_signing_provider"

const signedTestCommit = "45845b34bb6dff50ff93518db435c7d6944496ef"

type signingMockProvider struct {
	mockProvider
	verifyErr   error
	verifyCalls []string
}

func (m *signingMockProvider) VerifyCommitSignature(_ context.Context, _ *models.Repository, sha string, _ *commitsigning.TrustedKeys) (commitsigning.Signer, error) {
	m.verifyCalls = append(m.verifyCalls, sha)
	return commitsigning.Signer{}, m.verifyErr
}

func seedSigningRepo(t *testing.T) models.Repository {
	t.Helper()
	key, err := os.ReadFile("../commitsigning/testdata/ssh_trusted.pub")
	if err != nil {
		t.Fatalf("read key: %v", err)
	}
	repo := seedRepo(t)
	repo.Provider = testSigningProvider
	repo.RequireSignedCommits = true
	repo.CommitSigningKeys = string(key)
	if err := db.DB.Save(&repo).Error; err != nil {
		t.Fatalf("save repository: %v", err)
	}
	return repo
}

func TestSyncRepository_SignedCommitEnqueuesJob(t *testing.T) {
	setupTestDB(t)
	q := setupSyncQueue(t)
	repo := seedSigningRepo(t)
	seedApp(t, repo.Id, seedAgent(t).Id, "compose: v1")
	provider := &signingMockProvider{latestCommit: repositories.CommitInfo{Hash: signedTestCommit, Message: "signed"}}
	repositories.Register(testSigningProvider, provider)

	nop := zerolog.Nop()
	SyncRepository(t.Context(), &repo, SyncOrigin{Source: models.ApplicationEventSourceManual}, &nop)

	if len(provider.verifyCalls) != 1 || provider.verifyCalls[0] != signedTestCommit {
		t.Fatalf("expected one verification of %s, got %v", signedTestCommit, provider.verifyCalls)
	}
	if len(q.jobs) != 1 {
		t.Fatalf("expected 1 job in queue, got %d", len(q.jobs))
	}
}

func TestSyncRepository_UnsignedCommitIsRejected(t *testing.T) {
	setupTestDB(t)
	q := setupSyncQueue(t)
	repo := seedSigningRepo(t)
	app := seedApp(t, repo.Id, seedAgent(t).Id, "compose: v1")
	repositories.Register(testSigningProvider, &signingMockProvider{
		latestCommit: repositories.CommitInfo{Hash: signedTestCommit, Message: "unsigned"},
		verifyErr:    commitsigning.ErrUnsigned,
	})

	nop := zerolog.Nop()
	SyncRepository(t.Context(), &repo, SyncOrigin{Source: models.ApplicationEventSourceRepositoryWebhook}, &nop)

	if len(q.jobs) != 0 {
		t.Fatalf("expected no queued job for an unsigned commit, got %d", len(q.jobs))
	}
	got, err := gorm.G[models.Repository](db.DB).Where("id = ?", repo.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load repository: %v", err)
	}
	if got.SyncStatus != models.SyncStatusFailed || got.LastSyncError == nil || !strings.Contains(*got.LastSyncError, "commit is not signed") {
		t.Fatalf("expected failed sync with signature error, got %s / %v", got.SyncStatus, got.LastSyncError)
	}

	events := loadAppEvents(t, app.Id)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Status != models.ApplicationEventFailed || events[0].CommitHash == nil || *events[0].CommitHash != signedTestCommit {
		t.Fatalf("expected failed event for the rejected commit, got %+v", events[0])
	}
}

func TestSyncRepository_PollingDeployedCommitSkipsVerification(t *testing.T) {
	setupTestDB(t)
	setupSyncQueue(t)
	repo := seedSigningRepo(t)
	app := seedApp(t, repo.Id, seedAgent(t).Id, "compose: v1")
	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).
		Update(t.Context(), "commit", signedTestCommit); err != nil {
		t.Fatalf("update commit: %v", err)
	}
	provider := &signingMockProvider{
		latestCommit: repositories.CommitInfo{Hash: signedTestCommit},
		verifyErr:    commitsigning.ErrUnsigned,
	}
	repositories.Register(testSigningProvider, provider)

	nop := zerolog.Nop()
	SyncRepository(t.Context(), &repo, SyncOrigin{Source: models.ApplicationEventSourceRepositoryPolling}, &nop)

	if len(provider.verifyCalls) != 0 {
		t.Fatalf("expected no verification for an already deployed commit, got %v", provider.verifyCalls)
	}
}

func TestSyncRepository_RequireSignedCommitsWithoutProviderSupport(t *testing.T) {
	setupTestDB(t)
	q := setupSyncQueue(t)
	repo := seedSigningRepo(t)
	repo.Provider = testSyncProvider
	seedApp(t, repo.Id, seedAgent(t).Id, "compose: v1")
	repositories.Register(testSyncProvider, &mockProvider{
		latestCommit: repositories.CommitInfo{Hash: signedTestCommit},
	})

	nop := zerolog.Nop()
	SyncRepository(t.Context(), &repo, SyncOrigin{Source: models.ApplicationEventSourceManual}, &nop)

	if len(q.jobs) != 0 {
		t.Fatalf("expected no queued job, got %d", len(q.jobs))
	}
}

func TestVerifyCommitPolicy(t *testing.T) {
	provider := &signingMockProvider{}
	repo := models.Repository{Provider: testSigningProvider, RequireSignedCommits: true}

	if err := repositories.VerifyCommit(t.Context(), provider, &models.Repository{}, "main"); err != nil {
		t.Fatalf("expected no check when signing is not required, got %v", err)
	}
	if err := repositories.VerifyCommit(t.Context(), provider, &repo, signedTestCommit); err == nil || !strings.Contains(err.Error(), "no trusted signing keys") {
		t.Fatalf("expected missing keys error, got %v", err)
	}
	repo.CommitSigningKeys = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAILzLKC57O0T9rFN/f1l8+X4tAzHb1b8+LMaAJoDFAQ9/"
	for _, ref := range []string{"main", signedTestCommit[:12]} {
		if err := repositories.VerifyCommit(t.Context(), provider, &repo, ref); err == nil || !strings.Contains(err.Error(), "full commit hash") {
			t.Fatalf("expected full hash error for %q, got %v", ref, err)
		}
	}
	if len(provider.verifyCalls) != 0 {
		t.Fatalf("provider must not be called for rejected input, got %v", provider.verifyCalls)
	}
	if err := repositories.VerifyCommit(t.Context(), provider, &repo, signedTestCommit); err != nil {
		t.Fatalf("expected verified commit, got %v", err)
	}
}
