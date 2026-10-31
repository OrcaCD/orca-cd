package applicationevents

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/rs/zerolog"
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
		&models.ApplicationDeployment{},
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
		Id:    "user-1",
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

func TestActivityTopicSkipsRecordedNoChangeEvents(t *testing.T) {
	app, _ := setupEventTestDB(t)
	logger := zerolog.Nop()
	previousBroker := sse.DefaultBroker
	sse.DefaultBroker = sse.NewBroker(&logger)
	t.Cleanup(func() { sse.DefaultBroker = previousBroker })
	connID, updates := sse.DefaultBroker.Subscribe()
	t.Cleanup(func() { sse.DefaultBroker.Unsubscribe(connID) })

	params := Params{
		ApplicationID: app.Id,
		Type:          models.ApplicationEventCommitSync,
		Source:        models.ApplicationEventSourceRepositoryPolling,
	}
	eventsOnly := []string{sse.TopicApplicationEvents(app.Id).URL()}
	withActivity := []string{sse.TopicApplicationEvents(app.Id).URL(), sse.TopicApplicationActivity.URL()}
	expectUpdate := func(step string, want []string) {
		t.Helper()
		if got := (<-updates).URLs; !slices.Equal(got, want) {
			t.Fatalf("%s published %v, want %v", step, got, want)
		}
	}

	if _, err := RecordCompleted(t.Context(), params, models.ApplicationEventNoChange, nil); err != nil {
		t.Fatalf("RecordCompleted no_change: %v", err)
	}
	expectUpdate("recorded no_change", eventsOnly)

	if _, err := RecordCompleted(t.Context(), params, models.ApplicationEventSucceeded, nil); err != nil {
		t.Fatalf("RecordCompleted succeeded: %v", err)
	}
	expectUpdate("recorded succeeded", withActivity)

	// A running event is part of the activity, so completing it as no_change
	// still has to refresh it.
	requestID := "request-no-change"
	params.RequestID = &requestID
	if _, err := Start(t.Context(), params); err != nil {
		t.Fatalf("Start: %v", err)
	}
	expectUpdate("started", withActivity)
	matched, err := Complete(t.Context(), requestID, app.Id, models.ApplicationEventNoChange, nil)
	if err != nil || !matched {
		t.Fatalf("Complete matched=%v err=%v", matched, err)
	}
	expectUpdate("completed as no_change", withActivity)
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
		if _, err := RecordCompleted(ctx, params, models.ApplicationEventNoChange, nil); err != nil {
			t.Fatalf("RecordCompleted: %v", err)
		}
	}
	if _, err := RecordCompleted(ctx, Params{
		ApplicationID: otherApp.Id,
		Type:          models.ApplicationEventDeployment,
		Source:        models.ApplicationEventSourceManual,
	}, models.ApplicationEventSucceeded, nil); err != nil {
		t.Fatalf("RecordCompleted second application: %v", err)
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
	completedMessage := "completed event failure"
	completed, err := RecordCompleted(ctx, Params{
		ApplicationID: app.Id,
		Type:          models.ApplicationEventImageUpdate,
		Source:        models.ApplicationEventSourceImagePolling,
	}, models.ApplicationEventFailed, &completedMessage)
	if err != nil {
		t.Fatalf("RecordCompleted: %v", err)
	}
	completedAt := *completed.CompletedAt

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

	unchanged, err := gorm.G[models.ApplicationEvent](db.DB).Where("id = ?", completed.Id).First(ctx)
	if err != nil {
		t.Fatalf("load completed event: %v", err)
	}
	if unchanged.Status != models.ApplicationEventFailed ||
		unchanged.CompletedAt == nil ||
		!unchanged.CompletedAt.Equal(completedAt) ||
		unchanged.ErrorMessage == nil ||
		*unchanged.ErrorMessage != completedMessage {
		t.Fatalf("completed event changed during recovery: %+v", unchanged)
	}
}

func TestDeletingApplicationCascadesEvents(t *testing.T) {
	app, _ := setupEventTestDB(t)
	ctx := t.Context()
	if _, err := RecordCompleted(ctx, Params{
		ApplicationID: app.Id,
		Type:          models.ApplicationEventDeployment,
		Source:        models.ApplicationEventSourceManual,
	}, models.ApplicationEventSucceeded, nil); err != nil {
		t.Fatalf("RecordCompleted: %v", err)
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

func TestRecordCompletedRejectsRunningStatus(t *testing.T) {
	app, _ := setupEventTestDB(t)
	if _, err := RecordCompleted(t.Context(), Params{
		ApplicationID: app.Id,
		Type:          models.ApplicationEventDeployment,
		Source:        models.ApplicationEventSourceManual,
	}, models.ApplicationEventRunning, nil); err == nil {
		t.Fatal("RecordCompleted with running status succeeded")
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

func TestSourceForRequest(t *testing.T) {
	app, otherApp := setupEventTestDB(t)
	runningRequestID := "source-running"
	completedRequestID := "source-completed"
	if _, err := Start(t.Context(), Params{
		ApplicationID: app.Id,
		RequestID:     &runningRequestID,
		Type:          models.ApplicationEventCommitSync,
		Source:        models.ApplicationEventSourceRepositoryWebhook,
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := RecordCompleted(t.Context(), Params{
		ApplicationID: app.Id,
		RequestID:     &completedRequestID,
		Type:          models.ApplicationEventImageUpdate,
		Source:        models.ApplicationEventSourceImageWebhook,
	}, models.ApplicationEventSucceeded, nil); err != nil {
		t.Fatalf("RecordCompleted: %v", err)
	}
	// Events without a request ID must not match an empty request ID.
	if _, err := RecordCompleted(t.Context(), Params{
		ApplicationID: app.Id,
		Type:          models.ApplicationEventImageUpdate,
		Source:        models.ApplicationEventSourceImagePolling,
	}, models.ApplicationEventSucceeded, nil); err != nil {
		t.Fatalf("RecordCompleted without request ID: %v", err)
	}

	tests := []struct {
		name          string
		requestID     string
		applicationID string
		wantSource    models.ApplicationEventSource
		wantFound     bool
	}{
		{"running event", runningRequestID, app.Id, models.ApplicationEventSourceRepositoryWebhook, true},
		{"completed event", completedRequestID, app.Id, models.ApplicationEventSourceImageWebhook, true},
		{"other application", runningRequestID, otherApp.Id, "", false},
		{"unknown request", "unknown-request", app.Id, "", false},
		{"empty request ID", "", app.Id, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, found, err := SourceForRequest(t.Context(), tt.requestID, tt.applicationID)
			if err != nil {
				t.Fatalf("SourceForRequest: %v", err)
			}
			if found != tt.wantFound || source != tt.wantSource {
				t.Fatalf("SourceForRequest() = (%q, %v), want (%q, %v)", source, found, tt.wantSource, tt.wantFound)
			}
		})
	}
}

func TestSourceForRequestStaysCorrectAfterCompletion(t *testing.T) {
	app, _ := setupEventTestDB(t)
	requestID := "source-complete-later"
	if _, err := Start(t.Context(), Params{
		ApplicationID: app.Id,
		RequestID:     &requestID,
		Type:          models.ApplicationEventDeployment,
		Source:        models.ApplicationEventSourceManual,
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if matched, err := Complete(t.Context(), requestID, app.Id, models.ApplicationEventFailed, nil); err != nil || !matched {
		t.Fatalf("Complete matched=%v err=%v", matched, err)
	}

	source, found, err := SourceForRequest(t.Context(), requestID, app.Id)
	if err != nil || !found || source != models.ApplicationEventSourceManual {
		t.Fatalf("SourceForRequest() = (%q, %v, %v), want (%q, true, nil)", source, found, err, models.ApplicationEventSourceManual)
	}
}

func TestSourceForRequestReturnsDatabaseError(t *testing.T) {
	app, _ := setupEventTestDB(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	source, found, err := SourceForRequest(ctx, "request-db-error", app.Id)
	if err == nil {
		t.Fatal("SourceForRequest with canceled context returned no error")
	}
	if found || source != "" {
		t.Fatalf("SourceForRequest() = (%q, %v), want (\"\", false) on error", source, found)
	}
}

func seedDeployment(t *testing.T, appID string, status models.ApplicationDeploymentStatus) models.ApplicationDeployment {
	t.Helper()
	deployment := models.ApplicationDeployment{ApplicationId: appID, Status: status, Images: []models.DeployedImage{}}
	if err := gorm.G[models.ApplicationDeployment](db.DB).Create(t.Context(), &deployment); err != nil {
		t.Fatalf("seed deployment: %v", err)
	}
	return deployment
}

func TestRecoverRunningKeepsEventsUnderHealthCheck(t *testing.T) {
	app, _ := setupEventTestDB(t)
	ctx := t.Context()
	verifying := seedDeployment(t, app.Id, models.DeploymentVerifying)
	pending := seedDeployment(t, app.Id, models.DeploymentPending)
	gated, err := Start(ctx, Params{ApplicationID: app.Id, Type: models.ApplicationEventDeployment, Source: models.ApplicationEventSourceManual, DeploymentID: &verifying.Id})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	interrupted, err := Start(ctx, Params{ApplicationID: app.Id, Type: models.ApplicationEventDeployment, Source: models.ApplicationEventSourceManual, DeploymentID: &pending.Id})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if affected, err := RecoverRunning(ctx, "hub restarted"); err != nil || affected != 1 {
		t.Fatalf("RecoverRunning = %d, %v; want 1", affected, err)
	}

	for id, want := range map[string]models.ApplicationEventStatus{gated.Id: models.ApplicationEventRunning, interrupted.Id: models.ApplicationEventFailed} {
		event, err := gorm.G[models.ApplicationEvent](db.DB).Where("id = ?", id).First(ctx)
		if err != nil {
			t.Fatalf("load event: %v", err)
		}
		if event.Status != want {
			t.Errorf("event %s status = %q, want %q", id, event.Status, want)
		}
	}
}
