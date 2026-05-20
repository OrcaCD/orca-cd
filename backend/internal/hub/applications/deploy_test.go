package applications

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	hubws "github.com/OrcaCD/orca-cd/internal/hub/websocket"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// Mock deployment handle for testing
type mockDeploymentHandle struct {
	result *messages.DeployResult
	err    error
}

func (m mockDeploymentHandle) Await(context.Context) (*messages.DeployResult, error) {
	return m.result, m.err
}

func (m mockDeploymentHandle) Cancel() {}

// Mock transport for testing
type mockDeployTransport struct {
	startErr error
}

func (m *mockDeployTransport) StartDeploy(agentID string, req *messages.DeployRequest) (*hubws.DeployHandle, error) {
	// For testing, we return nil since we're testing at a higher level
	// The actual hubws.DeployHandle would be created by the real hub
	return nil, m.startErr
}

func seedTestApp(t *testing.T, repoID string) models.Application {
	t.Helper()
	return seedApp(t, repoID, "agent-1", "version: '3.9'\n")
}

func TestNewDeployer(t *testing.T) {
	setupTestDB(t)
	log := zerolog.Nop()
	hub := hubws.NewHub(&log)

	deployer := NewDeployer(hub, &log)

	if deployer == nil {
		t.Fatal("expected deployer to be created")
	}
	if deployer.log != &log {
		t.Error("expected logger to be set")
	}
}

func TestStartDeploy_NilApplication(t *testing.T) {
	setupTestDB(t)
	log := zerolog.Nop()

	transport := &mockDeployTransport{}
	deployer := &Deployer{
		log:       &log,
		transport: transport,
	}

	handle, err := deployer.StartDeploy(nil, "compose")

	if err == nil {
		t.Fatal("expected error for nil application")
	}
	if handle != nil {
		t.Error("expected no handle to be returned")
	}
}

func TestStartDeploy_MissingAgentId(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	log := zerolog.Nop()

	app := models.Application{
		Name:                crypto.EncryptedString("test-app"),
		RepositoryId:        repo.Id,
		AgentId:             "", // Missing
		SyncStatus:          models.UnknownSync,
		HealthStatus:        models.UnknownHealth,
		Branch:              "main",
		Path:                "docker-compose.yml",
		ComposeFile:         crypto.EncryptedString("version: '3.9'\n"),
		PreviousComposeFile: crypto.EncryptedString(""),
	}

	transport := &mockDeployTransport{}
	deployer := &Deployer{
		log:       &log,
		transport: transport,
	}

	handle, err := deployer.StartDeploy(&app, "compose")

	if err == nil {
		t.Fatal("expected error for missing agent id")
	}
	if handle != nil {
		t.Error("expected no handle to be returned")
	}
}

func TestStartDeploy_EmptyComposeFile(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	log := zerolog.Nop()

	app := models.Application{
		Name:                crypto.EncryptedString("test-app"),
		RepositoryId:        repo.Id,
		AgentId:             "agent-1",
		SyncStatus:          models.UnknownSync,
		HealthStatus:        models.UnknownHealth,
		Branch:              "main",
		Path:                "docker-compose.yml",
		ComposeFile:         crypto.EncryptedString(""), // Empty
		PreviousComposeFile: crypto.EncryptedString(""),
	}

	transport := &mockDeployTransport{}
	deployer := &Deployer{
		log:       &log,
		transport: transport,
	}

	handle, err := deployer.StartDeploy(&app, "")

	if err == nil {
		t.Fatal("expected error for empty compose file")
	}
	if handle != nil {
		t.Error("expected no handle to be returned")
	}
}

func TestStartDeploy_AgentUnavailable(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	app := seedTestApp(t, repo.Id)
	log := zerolog.Nop()

	transport := &mockDeployTransport{
		startErr: hubws.ErrDeployUnavailable,
	}
	deployer := &Deployer{
		log:       &log,
		transport: transport,
	}

	handle, err := deployer.StartDeploy(&app, app.ComposeFile.String())

	if err == nil {
		t.Fatal("expected error for unavailable agent")
	}
	if !errors.Is(err, ErrAgentUnavailable) {
		t.Errorf("expected ErrAgentUnavailable, got %v", err)
	}
	if handle != nil {
		t.Error("expected no handle to be returned")
	}
}

func TestTrackManualDeploy_Success(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	app := seedTestApp(t, repo.Id)
	log := zerolog.Nop()

	result := &messages.DeployResult{
		RequestId:    "req-1",
		Success:      true,
		ErrorMessage: "",
	}

	mockHandle := &mockDeploymentHandle{result: result}
	deployer := &Deployer{
		log:       &log,
		transport: &mockDeployTransport{},
	}

	// Start tracking in goroutine
	deployer.TrackManualDeploy(app, mockHandle)

	// Give it time to complete
	time.Sleep(100 * time.Millisecond)

	// Verify app status was updated to Synced
	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(context.Background())
	if err != nil {
		t.Fatalf("failed to fetch app: %v", err)
	}
	if updated.SyncStatus != models.Synced {
		t.Errorf("expected sync status %q, got %q", models.Synced, updated.SyncStatus)
	}
	if updated.HealthStatus != models.Healthy {
		t.Errorf("expected health status %q, got %q", models.Healthy, updated.HealthStatus)
	}
}

func TestTrackManualDeploy_ExecutionFailure(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	app := seedTestApp(t, repo.Id)
	log := zerolog.Nop()

	result := &messages.DeployResult{
		RequestId:    "req-1",
		Success:      false,
		ErrorMessage: "deployment failed on agent",
	}

	mockHandle := &mockDeploymentHandle{result: result}
	deployer := &Deployer{
		log:       &log,
		transport: &mockDeployTransport{},
	}

	deployer.TrackManualDeploy(app, mockHandle)
	time.Sleep(100 * time.Millisecond)

	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(context.Background())
	if err != nil {
		t.Fatalf("failed to fetch app: %v", err)
	}
	if updated.SyncStatus != models.OutOfSync {
		t.Errorf("expected sync status %q, got %q", models.OutOfSync, updated.SyncStatus)
	}
	if updated.HealthStatus != models.Unhealthy {
		t.Errorf("expected health status %q, got %q", models.Unhealthy, updated.HealthStatus)
	}
}

func TestTrackManualDeploy_TransportError(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	app := seedTestApp(t, repo.Id)
	log := zerolog.Nop()

	mockHandle := &mockDeploymentHandle{
		err: errors.New("transport error"),
	}
	deployer := &Deployer{
		log:       &log,
		transport: &mockDeployTransport{},
	}

	deployer.TrackManualDeploy(app, mockHandle)
	time.Sleep(100 * time.Millisecond)

	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(context.Background())
	if err != nil {
		t.Fatalf("failed to fetch app: %v", err)
	}
	if updated.SyncStatus != models.OutOfSync {
		t.Errorf("expected sync status %q, got %q", models.OutOfSync, updated.SyncStatus)
	}
}

func TestTrackManualDeploy_NilResult(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	app := seedTestApp(t, repo.Id)
	log := zerolog.Nop()

	mockHandle := &mockDeploymentHandle{result: nil}
	deployer := &Deployer{
		log:       &log,
		transport: &mockDeployTransport{},
	}

	deployer.TrackManualDeploy(app, mockHandle)
	time.Sleep(100 * time.Millisecond)

	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(context.Background())
	if err != nil {
		t.Fatalf("failed to fetch app: %v", err)
	}
	if updated.SyncStatus != models.OutOfSync {
		t.Errorf("expected sync status %q, got %q", models.OutOfSync, updated.SyncStatus)
	}
}

func TestMarkDeploymentInProgress(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	app := seedTestApp(t, repo.Id)
	log := zerolog.Nop()

	err := markDeploymentInProgress(context.Background(), app.Id, &log)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(context.Background())
	if err != nil {
		t.Fatalf("failed to fetch app: %v", err)
	}
	if updated.SyncStatus != models.Syncing {
		t.Errorf("expected sync status %q, got %q", models.Syncing, updated.SyncStatus)
	}
}

func TestMarkDeploymentSuccess(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	app := seedTestApp(t, repo.Id)
	log := zerolog.Nop()

	markDeploymentSuccess(context.Background(), app.Id, nil, &log)

	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(context.Background())
	if err != nil {
		t.Fatalf("failed to fetch app: %v", err)
	}
	if updated.SyncStatus != models.Synced {
		t.Errorf("expected sync status %q, got %q", models.Synced, updated.SyncStatus)
	}
	if updated.HealthStatus != models.Healthy {
		t.Errorf("expected health status %q, got %q", models.Healthy, updated.HealthStatus)
	}
	if updated.LastSyncedAt == nil {
		t.Error("expected LastSyncedAt to be set")
	}
}

func TestMarkDeploymentSuccess_WithUpdateFn(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	app := seedTestApp(t, repo.Id)
	log := zerolog.Nop()

	updateFn := func(a *models.Application) {
		a.Commit = "new-commit-hash"
	}

	markDeploymentSuccess(context.Background(), app.Id, updateFn, &log)

	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(context.Background())
	if err != nil {
		t.Fatalf("failed to fetch app: %v", err)
	}
	if updated.Commit != "new-commit-hash" {
		t.Errorf("expected commit %q, got %q", "new-commit-hash", updated.Commit)
	}
}

func TestMarkDeploymentExecutionFailure(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	app := seedTestApp(t, repo.Id)
	log := zerolog.Nop()

	markDeploymentExecutionFailure(context.Background(), app.Id, &log)

	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(context.Background())
	if err != nil {
		t.Fatalf("failed to fetch app: %v", err)
	}
	if updated.SyncStatus != models.OutOfSync {
		t.Errorf("expected sync status %q, got %q", models.OutOfSync, updated.SyncStatus)
	}
	if updated.HealthStatus != models.Unhealthy {
		t.Errorf("expected health status %q, got %q", models.Unhealthy, updated.HealthStatus)
	}
}

func TestMarkDeploymentTransportError(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	app := seedTestApp(t, repo.Id)
	log := zerolog.Nop()

	markDeploymentTransportError(context.Background(), app.Id, &log)

	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(context.Background())
	if err != nil {
		t.Fatalf("failed to fetch app: %v", err)
	}
	if updated.SyncStatus != models.OutOfSync {
		t.Errorf("expected sync status %q, got %q", models.OutOfSync, updated.SyncStatus)
	}
}
