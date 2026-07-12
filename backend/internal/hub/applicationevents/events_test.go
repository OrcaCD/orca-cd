package applicationevents

import (
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func setupEventTestDB(t *testing.T) (models.Application, models.Application) {
	t.Helper()

	testDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "events.db")+"?_foreign_keys=on"), &gorm.Config{
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
		t.Fatalf("open test db: %v", err)
	}
	if err := testDB.AutoMigrate(
		&models.User{},
		&models.Repository{},
		&models.Agent{},
		&models.Application{},
		&models.ApplicationEvent{},
	); err != nil {
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
	user := models.User{
		Base:  models.Base{Id: "user-1"},
		Email: "alex@example.com",
		Name:  "Alex",
		Role:  models.UserRoleAdmin,
	}
	if err := gorm.G[models.User](testDB).Create(ctx, &user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	repository := models.Repository{
		Name:       "owner/repository",
		Url:        "https://example.com/owner/repository.git",
		Provider:   models.Generic,
		AuthMethod: models.AuthMethodNone,
		SyncType:   models.SyncTypeManual,
		SyncStatus: models.SyncStatusUnknown,
		CreatedBy:  user.Id,
	}
	if err := gorm.G[models.Repository](testDB).Create(ctx, &repository); err != nil {
		t.Fatalf("seed repository: %v", err)
	}
	agent := models.Agent{
		Name:  crypto.EncryptedString("test-agent"),
		KeyId: crypto.EncryptedString("test-key"),
	}
	if err := gorm.G[models.Agent](testDB).Create(ctx, &agent); err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	applications := []models.Application{
		{
			Name:                crypto.EncryptedString("application-one"),
			RepositoryId:        repository.Id,
			AgentId:             agent.Id,
			SyncStatus:          models.UnknownSync,
			HealthStatus:        models.UnknownHealth,
			Branch:              "main",
			Path:                "compose-one.yaml",
			ComposeFile:         crypto.EncryptedString("services: {}"),
			PreviousComposeFile: crypto.EncryptedString(""),
		},
		{
			Name:                crypto.EncryptedString("application-two"),
			RepositoryId:        repository.Id,
			AgentId:             agent.Id,
			SyncStatus:          models.UnknownSync,
			HealthStatus:        models.UnknownHealth,
			Branch:              "main",
			Path:                "compose-two.yaml",
			ComposeFile:         crypto.EncryptedString("services: {}"),
			PreviousComposeFile: crypto.EncryptedString(""),
		},
	}
	for i := range applications {
		if err := gorm.G[models.Application](testDB).Create(ctx, &applications[i]); err != nil {
			t.Fatalf("seed application %d: %v", i, err)
		}
	}

	return applications[0], applications[1]
}

func TestStartAndCompleteCorrelatesRequestAndApplication(t *testing.T) {
	app, _ := setupEventTestDB(t)
	requestID := "request-1"
	actorID, actorName := "user-1", "Alex"
	event, err := Start(t.Context(), Params{
		ApplicationID: app.Id,
		RequestID:     &requestID,
		Type:          models.ApplicationEventDeployment,
		Source:        models.ApplicationEventSourceManual,
		ActorUserID:   &actorID,
		ActorName:     &actorName,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if event.Status != models.ApplicationEventRunning {
		t.Fatalf("status = %q", event.Status)
	}

	matched, err := Complete(t.Context(), requestID, app.Id, models.ApplicationEventSucceeded, nil)
	if err != nil || !matched {
		t.Fatalf("Complete matched=%v err=%v", matched, err)
	}
	got, err := gorm.G[models.ApplicationEvent](db.DB).Where("id = ?", event.Id).First(t.Context())
	if err != nil {
		t.Fatalf("load event: %v", err)
	}
	if got.Status != models.ApplicationEventSucceeded || got.CompletedAt == nil {
		t.Fatalf("event not completed: %+v", got)
	}
}

func TestCompleteRejectsWrongApplicationAndDuplicateResult(t *testing.T) {
	app, _ := setupEventTestDB(t)
	requestID := "request-2"
	_, _ = Start(t.Context(), Params{
		ApplicationID: app.Id,
		RequestID:     &requestID,
		Type:          models.ApplicationEventDeployment,
		Source:        models.ApplicationEventSourceManual,
	})
	matched, err := Complete(t.Context(), requestID, "other-app", models.ApplicationEventSucceeded, nil)
	if err != nil || matched {
		t.Fatalf("wrong application matched=%v err=%v", matched, err)
	}
	matched, err = Complete(t.Context(), requestID, app.Id, models.ApplicationEventSucceeded, nil)
	if err != nil || !matched {
		t.Fatalf("first completion matched=%v err=%v", matched, err)
	}
	matched, err = Complete(t.Context(), requestID, app.Id, models.ApplicationEventFailed, new("late"))
	if err != nil || matched {
		t.Fatalf("duplicate completion matched=%v err=%v", matched, err)
	}
}

func TestRetentionKeepsNewestThousandPerApplication(t *testing.T) {
	app, otherApp := setupEventTestDB(t)
	ctx := t.Context()
	params := Params{
		ApplicationID: app.Id,
		Type:          models.ApplicationEventCommitSync,
		Source:        models.ApplicationEventSourceRepositoryPolling,
	}
	for range MaxPerApplication + 1 {
		if _, err := RecordTerminal(ctx, params, models.ApplicationEventNoChange, nil); err != nil {
			t.Fatalf("RecordTerminal: %v", err)
		}
	}
	if _, err := RecordTerminal(ctx, Params{
		ApplicationID: otherApp.Id,
		Type:          models.ApplicationEventDeployment,
		Source:        models.ApplicationEventSourceManual,
	}, models.ApplicationEventSucceeded, nil); err != nil {
		t.Fatalf("RecordTerminal second application: %v", err)
	}

	count, err := gorm.G[models.ApplicationEvent](db.DB).
		Where("application_id = ?", app.Id).
		Count(ctx, "id")
	if err != nil {
		t.Fatalf("count retained events: %v", err)
	}
	if count != MaxPerApplication {
		t.Fatalf("retained event count = %d, want %d", count, MaxPerApplication)
	}
	otherCount, err := gorm.G[models.ApplicationEvent](db.DB).
		Where("application_id = ?", otherApp.Id).
		Count(ctx, "id")
	if err != nil {
		t.Fatalf("count second application events: %v", err)
	}
	if otherCount != 1 {
		t.Fatalf("second application event count = %d, want 1", otherCount)
	}
}

func TestRecoverRunningFailsOnlyRunningEvents(t *testing.T) {
	app, otherApp := setupEventTestDB(t)
	ctx := t.Context()
	for _, applicationID := range []string{app.Id, otherApp.Id} {
		if _, err := Start(ctx, Params{
			ApplicationID: applicationID,
			Type:          models.ApplicationEventDeployment,
			Source:        models.ApplicationEventSourceManual,
		}); err != nil {
			t.Fatalf("Start: %v", err)
		}
	}
	terminalMessage := "terminal event failure"
	terminal, err := RecordTerminal(ctx, Params{
		ApplicationID: app.Id,
		Type:          models.ApplicationEventImageUpdate,
		Source:        models.ApplicationEventSourceImagePolling,
	}, models.ApplicationEventFailed, &terminalMessage)
	if err != nil {
		t.Fatalf("RecordTerminal: %v", err)
	}
	terminalCompletedAt := *terminal.CompletedAt

	const recoveryMessage = "hub restarted while event was running"
	affected, err := RecoverRunning(ctx, recoveryMessage)
	if err != nil {
		t.Fatalf("RecoverRunning: %v", err)
	}
	if affected != 2 {
		t.Fatalf("recovered rows = %d, want 2", affected)
	}

	runningEvents, err := gorm.G[models.ApplicationEvent](db.DB).
		Where("type = ?", models.ApplicationEventDeployment).
		Find(ctx)
	if err != nil {
		t.Fatalf("load recovered events: %v", err)
	}
	if len(runningEvents) != 2 {
		t.Fatalf("recovered event count = %d, want 2", len(runningEvents))
	}
	for _, event := range runningEvents {
		if event.Status != models.ApplicationEventFailed || event.CompletedAt == nil {
			t.Fatalf("event not recovered: %+v", event)
		}
		if event.ErrorMessage == nil || *event.ErrorMessage != recoveryMessage {
			t.Fatalf("recovery error message = %v", event.ErrorMessage)
		}
	}
	if !runningEvents[0].CompletedAt.Equal(*runningEvents[1].CompletedAt) {
		t.Fatalf("recovered events do not share completion time: %v, %v", runningEvents[0].CompletedAt, runningEvents[1].CompletedAt)
	}

	unchanged, err := gorm.G[models.ApplicationEvent](db.DB).Where("id = ?", terminal.Id).First(ctx)
	if err != nil {
		t.Fatalf("load terminal event: %v", err)
	}
	if unchanged.Status != models.ApplicationEventFailed ||
		unchanged.CompletedAt == nil ||
		!unchanged.CompletedAt.Equal(terminalCompletedAt) ||
		unchanged.ErrorMessage == nil ||
		*unchanged.ErrorMessage != terminalMessage {
		t.Fatalf("terminal event changed during recovery: %+v", unchanged)
	}
}

func TestDeletingApplicationCascadesEvents(t *testing.T) {
	app, _ := setupEventTestDB(t)
	ctx := t.Context()
	if _, err := RecordTerminal(ctx, Params{
		ApplicationID: app.Id,
		Type:          models.ApplicationEventDeployment,
		Source:        models.ApplicationEventSourceManual,
	}, models.ApplicationEventSucceeded, nil); err != nil {
		t.Fatalf("RecordTerminal: %v", err)
	}
	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).Delete(ctx); err != nil {
		t.Fatalf("delete application: %v", err)
	}

	count, err := gorm.G[models.ApplicationEvent](db.DB).
		Where("application_id = ?", app.Id).
		Count(ctx, "id")
	if err != nil {
		t.Fatalf("count application events: %v", err)
	}
	if count != 0 {
		t.Fatalf("application event count after delete = %d, want 0", count)
	}
}

func TestRecordTerminalRejectsRunningStatus(t *testing.T) {
	app, _ := setupEventTestDB(t)
	if _, err := RecordTerminal(t.Context(), Params{
		ApplicationID: app.Id,
		Type:          models.ApplicationEventDeployment,
		Source:        models.ApplicationEventSourceManual,
	}, models.ApplicationEventRunning, nil); err == nil {
		t.Fatal("RecordTerminal with running status succeeded")
	}
}

func TestCompleteRejectsRunningStatus(t *testing.T) {
	app, _ := setupEventTestDB(t)
	requestID := "request-running"
	if _, err := Start(t.Context(), Params{
		ApplicationID: app.Id,
		RequestID:     &requestID,
		Type:          models.ApplicationEventDeployment,
		Source:        models.ApplicationEventSourceManual,
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	matched, err := Complete(t.Context(), requestID, app.Id, models.ApplicationEventRunning, nil)
	if err == nil {
		t.Fatal("Complete with running status succeeded")
	}
	if matched {
		t.Fatal("Complete with running status matched an event")
	}
}

func TestPath(t *testing.T) {
	const applicationID = "application-1"
	if got, want := Path(applicationID), "/api/v1/applications/application-1/events"; got != want {
		t.Fatalf("Path() = %q, want %q", got, want)
	}
}
