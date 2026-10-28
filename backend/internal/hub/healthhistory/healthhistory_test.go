package healthhistory

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const testRepositoryID = "repository-1"

// setupTestDB seeds the agents agent-1 and agent-2 that applications can be
// assigned to.
func setupTestDB(t *testing.T) {
	t.Helper()

	testDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "health.db")+"?_foreign_keys=on"), &gorm.Config{
		Logger: gormlogger.Discard,
	})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := testDB.AutoMigrate(&models.User{}, &models.Repository{}, &models.Agent{}, &models.Application{}, &models.ApplicationHealthChange{}); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	sqlDB, err := testDB.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	previousDB := db.DB
	db.DB = testDB
	t.Cleanup(func() {
		db.DB = previousDB
		_ = sqlDB.Close()
	})

	if err := crypto.Init("test-secret-that-is-long-enough-32chars"); err != nil {
		t.Fatalf("init crypto: %v", err)
	}

	ctx := t.Context()
	if err := gorm.G[models.User](testDB).Create(ctx, &models.User{Id: "user-1", Email: "alex@example.com", Name: "Alex", Role: models.UserRoleAdmin}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := gorm.G[models.Repository](testDB).Create(ctx, &models.Repository{
		Id:         testRepositoryID,
		Name:       "owner/repository",
		Url:        "https://example.com/owner/repository.git",
		Provider:   models.Generic,
		AuthMethod: models.AuthMethodNone,
		SyncType:   models.SyncTypeManual,
		SyncStatus: models.SyncStatusUnknown,
		CreatedBy:  "user-1",
	}); err != nil {
		t.Fatalf("seed repository: %v", err)
	}
	for _, id := range []string{"agent-1", "agent-2"} {
		if err := gorm.G[models.Agent](testDB).Create(ctx, &models.Agent{
			Id:    id,
			Name:  crypto.EncryptedString(id),
			KeyId: crypto.EncryptedString("key-" + id),
		}); err != nil {
			t.Fatalf("seed agent: %v", err)
		}
	}
}

func seedApp(t *testing.T, agentID string) models.Application {
	t.Helper()
	app := models.Application{
		Name:         crypto.EncryptedString("app"),
		RepositoryId: testRepositoryID,
		AgentId:      agentID,
		SyncStatus:   models.Synced,
		HealthStatus: models.Healthy,
		Branch:       "main",
		Path:         "compose.yaml",
		ComposeFile:  crypto.EncryptedString("services: {}\n"),
	}
	if err := db.DB.Select("*").Create(&app).Error; err != nil {
		t.Fatalf("seed application: %v", err)
	}
	return app
}

func changesOf(t *testing.T, appID string) []models.ApplicationHealthChange {
	t.Helper()
	changes, err := gorm.G[models.ApplicationHealthChange](db.DB).
		Where("application_id = ?", appID).
		Order("changed_at, id").
		Find(context.Background())
	if err != nil {
		t.Fatalf("load health changes: %v", err)
	}
	return changes
}

func record(t *testing.T, change models.ApplicationHealthChange) bool {
	t.Helper()
	recorded, err := Record(t.Context(), change)
	if err != nil {
		t.Fatalf("Record() error: %v", err)
	}
	return recorded
}

func TestRecord_OnlyStoresChanges(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t, "agent-1")

	if record(t, models.ApplicationHealthChange{ApplicationId: app.Id, Status: models.UnknownHealth}) {
		t.Error("an unknown first status must not be recorded")
	}
	if !record(t, models.ApplicationHealthChange{ApplicationId: app.Id, Status: models.Healthy}) {
		t.Error("expected the first known status to be recorded")
	}
	if record(t, models.ApplicationHealthChange{ApplicationId: app.Id, Status: models.Healthy}) {
		t.Error("an unchanged status must not be recorded")
	}
	if !record(t, models.ApplicationHealthChange{ApplicationId: app.Id, Status: models.Degraded, Services: []models.ServiceHealth{{Name: "web", Status: models.Unhealthy}}}) {
		t.Error("expected the degraded status to be recorded")
	}
	if !record(t, models.ApplicationHealthChange{ApplicationId: app.Id, Status: models.UnknownHealth}) {
		t.Error("expected unknown after a known status to be recorded")
	}

	changes := changesOf(t, app.Id)
	if len(changes) != 3 {
		t.Fatalf("expected 3 changes, got %d", len(changes))
	}
	if changes[1].Status != models.Degraded || len(changes[1].Services) != 1 || changes[1].Services[0].Name != "web" {
		t.Errorf("expected degraded change with failing service web, got %+v", changes[1])
	}
	if changes[2].Status != models.UnknownHealth {
		t.Errorf("expected unknown last, got %q", changes[2].Status)
	}
}

func TestRecord_KeepsHistoryOrdered(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t, "agent-1")
	now := time.Now()

	record(t, models.ApplicationHealthChange{ApplicationId: app.Id, Status: models.Healthy, ChangedAt: now})
	record(t, models.ApplicationHealthChange{ApplicationId: app.Id, Status: models.Unhealthy, ChangedAt: now.Add(-time.Hour)})

	changes := changesOf(t, app.Id)
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(changes))
	}
	if !changes[1].ChangedAt.Equal(changes[0].ChangedAt) {
		t.Errorf("expected an older change to move to the latest change time, got %v and %v", changes[0].ChangedAt, changes[1].ChangedAt)
	}
	latest, err := Latest(t.Context(), app.Id)
	if err != nil {
		t.Fatalf("Latest() error: %v", err)
	}
	if latest.Status != models.Unhealthy {
		t.Errorf("expected the later recorded change to be the latest, got %q", latest.Status)
	}
}

func TestRecord_BoundsServices(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t, "agent-1")

	services := make([]models.ServiceHealth, maxServices+5)
	for i := range services {
		services[i] = models.ServiceHealth{Name: strings.Repeat("s", maxServiceNameLength+10), Status: models.Unhealthy}
	}
	record(t, models.ApplicationHealthChange{ApplicationId: app.Id, Status: models.Unhealthy, Services: services})

	stored := changesOf(t, app.Id)[0].Services
	if len(stored) != maxServices {
		t.Errorf("expected %d services, got %d", maxServices, len(stored))
	}
	if len(stored[0].Name) != maxServiceNameLength {
		t.Errorf("expected service name to be cut to %d bytes, got %d", maxServiceNameLength, len(stored[0].Name))
	}
}

func TestRecordAgentOffline(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t, "agent-1")
	otherApp := seedApp(t, "agent-1")
	foreignApp := seedApp(t, "agent-2")
	for _, id := range []string{app.Id, otherApp.Id, foreignApp.Id} {
		record(t, models.ApplicationHealthChange{ApplicationId: id, Status: models.Healthy, ChangedAt: time.Now().Add(-time.Hour)})
	}

	offlineSince := time.Now().Add(-time.Minute)
	if err := RecordAgentOffline(t.Context(), "agent-1", offlineSince); err != nil {
		t.Fatalf("RecordAgentOffline() error: %v", err)
	}

	for _, id := range []string{app.Id, otherApp.Id} {
		latest, err := Latest(t.Context(), id)
		if err != nil {
			t.Fatalf("Latest() error: %v", err)
		}
		if latest.Status != models.UnknownHealth || latest.Reason == nil || *latest.Reason != models.HealthChangeReasonAgentOffline {
			t.Errorf("expected agent offline change, got %+v", latest)
		}
		if !latest.ChangedAt.Equal(offlineSince.UTC()) {
			t.Errorf("expected change at %v, got %v", offlineSince, latest.ChangedAt)
		}
	}
	if latest, _ := Latest(t.Context(), foreignApp.Id); latest.Status != models.Healthy {
		t.Errorf("another agent's application must be untouched, got %q", latest.Status)
	}
}

func TestPrune_KeepsLatestChangeBeforeCutoff(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t, "agent-1")
	otherApp := seedApp(t, "agent-1")
	now := time.Now()

	record(t, models.ApplicationHealthChange{ApplicationId: app.Id, Status: models.Healthy, ChangedAt: now.Add(-Retention - 3*time.Hour)})
	record(t, models.ApplicationHealthChange{ApplicationId: app.Id, Status: models.Unhealthy, ChangedAt: now.Add(-Retention - 2*time.Hour)})
	record(t, models.ApplicationHealthChange{ApplicationId: app.Id, Status: models.Healthy, ChangedAt: now.Add(-Retention - time.Hour)})
	record(t, models.ApplicationHealthChange{ApplicationId: app.Id, Status: models.Stopped, ChangedAt: now.Add(-time.Hour)})
	record(t, models.ApplicationHealthChange{ApplicationId: otherApp.Id, Status: models.Healthy, ChangedAt: now.Add(-Retention - time.Hour)})

	deleted, err := Prune(t.Context(), now)
	if err != nil {
		t.Fatalf("Prune() error: %v", err)
	}
	if deleted != 2 {
		t.Errorf("expected 2 deleted changes, got %d", deleted)
	}

	changes := changesOf(t, app.Id)
	if len(changes) != 2 || changes[0].Status != models.Healthy || changes[1].Status != models.Stopped {
		t.Errorf("expected the last change before the cutoff and the recent change to remain, got %+v", changes)
	}
	if changes := changesOf(t, otherApp.Id); len(changes) != 1 {
		t.Errorf("expected the only change of another application to remain, got %d", len(changes))
	}
}

func TestHealthChangesAreDeletedWithApplication(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t, "agent-1")
	record(t, models.ApplicationHealthChange{ApplicationId: app.Id, Status: models.Healthy})

	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).Delete(t.Context()); err != nil {
		t.Fatalf("delete application: %v", err)
	}
	if changes := changesOf(t, app.Id); len(changes) != 0 {
		t.Errorf("expected health changes to be deleted with the application, got %d", len(changes))
	}
}
