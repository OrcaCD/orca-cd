package applications

import (
	"slices"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/composeconfig"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	application_deployer "github.com/OrcaCD/orca-cd/internal/hub/deployer"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

const composeWithoutConfig = "services:\n  app:\n    image: myimage:1.0\n"

func runComposeSync(t *testing.T, app models.Application, compose string) *stubDeployer {
	t.Helper()
	stub := &stubDeployer{}
	prev := application_deployer.DefaultApplicationDeployer
	application_deployer.DefaultApplicationDeployer = stub
	t.Cleanup(func() { application_deployer.DefaultApplicationDeployer = prev })

	repo, err := gorm.G[models.Repository](db.DB).Where("id = ?", app.RepositoryId).First(t.Context())
	if err != nil {
		t.Fatalf("load repository: %v", err)
	}
	nop := zerolog.Nop()
	processSyncJob(t.Context(), syncJob{
		Application:        app,
		Repository:         repo,
		RepositoryProvider: &mockProvider{fileContent: compose},
		Commit:             "newsha",
		CommitMessage:      "new commit",
		Origin:             SyncOrigin{Source: models.ApplicationEventSourceManual},
	}, &nop)
	return stub
}

func loadApp(t *testing.T, id string) models.Application {
	t.Helper()
	app, err := gorm.G[models.Application](db.DB).Where("id = ?", id).First(t.Context())
	if err != nil {
		t.Fatalf("load application: %v", err)
	}
	return app
}

func TestProcessSyncJob_ComposeConfig_AppliedBeforeDeploy(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t, seedRepo(t).Id, seedAgent(t).Id, composeWithoutConfig)
	compose := "x-orcacd:\n  icon: rocket\n  rollback:\n    enabled: true\n    health-timeout: 2m\n" +
		strings.Replace(composeWithoutConfig, "1.0", "2.0", 1)

	stub := runComposeSync(t, app, compose)

	if !stub.called {
		t.Fatal("expected a deployment")
	}
	if !stub.app.RollbackEnabled || stub.app.RollbackHealthTimeoutSeconds != 120 {
		t.Errorf("expected the deployment to use the rollback settings of the compose file, got %+v", stub.app)
	}
	updated := loadApp(t, app.Id)
	if updated.Icon != "rocket" || !updated.RollbackEnabled || updated.RollbackHealthTimeoutSeconds != 120 {
		t.Errorf("expected the settings to be saved, got %+v", updated)
	}
	want := []string{composeconfig.SettingIcon, composeconfig.SettingRollbackEnabled, composeconfig.SettingRollbackHealthTimeout}
	if !slices.Equal(updated.ManagedSettings, want) {
		t.Errorf("ManagedSettings = %v, want %v", updated.ManagedSettings, want)
	}
	if updated.ComposeFile.String() != compose || updated.Commit != "newsha" {
		t.Errorf("expected the compose file and commit to be saved, got %q at %s", updated.ComposeFile.String(), updated.Commit)
	}
}

// seedSyncedApp seeds an application with composeWithoutConfig whose last
// deployment succeeded.
func seedSyncedApp(t *testing.T) models.Application {
	t.Helper()
	app := seedApp(t, seedRepo(t).Id, seedAgent(t).Id, composeWithoutConfig)
	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).
		Updates(t.Context(), models.Application{SyncStatus: models.Synced}); err != nil {
		t.Fatal(err)
	}
	return loadApp(t, app.Id)
}

func TestProcessSyncJob_ComposeConfig_OnlySettingsChangedDoesNotDeploy(t *testing.T) {
	setupTestDB(t)
	app := seedSyncedApp(t)
	compose := "x-orcacd:\n  delete-old-images: true\n\n" + composeWithoutConfig

	stub := runComposeSync(t, app, compose)

	if stub.called {
		t.Error("expected no deployment when only x-orcacd changed")
	}
	updated := loadApp(t, app.Id)
	if !updated.DeleteOldImages || !slices.Equal(updated.ManagedSettings, []string{composeconfig.SettingDeleteOldImages}) {
		t.Errorf("expected the settings to be applied, got %+v", updated)
	}
	if updated.ComposeFile.String() != compose || updated.PreviousComposeFile.String() != composeWithoutConfig {
		t.Errorf("expected the new compose file to be saved, got %q", updated.ComposeFile.String())
	}
	if updated.SyncStatus != models.Synced || updated.Commit != "newsha" {
		t.Errorf("expected the application to be synced at the new commit, got %s at %s", updated.SyncStatus, updated.Commit)
	}
	events := loadAppEvents(t, app.Id)
	if len(events) != 1 || events[0].Status != models.ApplicationEventNoChange {
		t.Errorf("expected one no-change event, got %+v", events)
	}
}

func TestProcessSyncJob_ComposeConfig_UnchangedFileStillApplies(t *testing.T) {
	setupTestDB(t)
	// An application created before x-orcacd existed has the settings in its
	// file without having applied them.
	compose := "x-orcacd:\n  rollback: true\n" + composeWithoutConfig
	app := seedApp(t, seedRepo(t).Id, seedAgent(t).Id, compose)

	stub := runComposeSync(t, app, compose)

	if stub.called {
		t.Error("expected no deployment for an unchanged compose file")
	}
	if updated := loadApp(t, app.Id); !updated.RollbackEnabled {
		t.Errorf("expected rollback to be enabled, got %+v", updated)
	}
}

func TestProcessSyncJob_ComposeConfig_RemovedSettingGoesBackToDefault(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t, seedRepo(t).Id, seedAgent(t).Id, composeWithoutConfig)
	runComposeSync(t, app, "x-orcacd:\n  delete-old-images: true\n"+composeWithoutConfig)

	runComposeSync(t, loadApp(t, app.Id), composeWithoutConfig)

	updated := loadApp(t, app.Id)
	if updated.DeleteOldImages || len(updated.ManagedSettings) != 0 {
		t.Errorf("expected delete-old-images to go back to its default, got %+v", updated)
	}
}

func TestProcessSyncJob_ComposeConfig_InvalidFailsSync(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t, seedRepo(t).Id, seedAgent(t).Id, composeWithoutConfig)

	stub := runComposeSync(t, app, "x-orcacd:\n  rollback:\n    health-timeout: 2h\n"+composeWithoutConfig)

	if stub.called {
		t.Error("expected no deployment for an invalid x-orcacd")
	}
	updated := loadApp(t, app.Id)
	if updated.SyncStatus != models.OutOfSync || updated.LastSyncError == nil ||
		!strings.Contains(*updated.LastSyncError, "x-orcacd.rollback.health-timeout: must be at most 1h") {
		t.Errorf("expected the sync to fail with the x-orcacd error, got %s %v", updated.SyncStatus, updated.LastSyncError)
	}
	if updated.Commit != app.Commit || updated.ComposeFile.String() != composeWithoutConfig {
		t.Error("expected the commit and compose file to stay unchanged")
	}
	events := loadAppEvents(t, app.Id)
	if len(events) != 1 || events[0].Status != models.ApplicationEventFailed {
		t.Errorf("expected one failed event, got %+v", events)
	}
}

func TestProcessSyncJob_ComposeConfig_OnlySettingsChangedDeploysWhenOutOfSync(t *testing.T) {
	setupTestDB(t)
	// The last deployment failed, so a commit that only changes x-orcacd
	// must not mark the application synced without deploying.
	app := seedApp(t, seedRepo(t).Id, seedAgent(t).Id, composeWithoutConfig)
	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).
		Updates(t.Context(), models.Application{SyncStatus: models.OutOfSync}); err != nil {
		t.Fatal(err)
	}

	stub := runComposeSync(t, loadApp(t, app.Id), "x-orcacd:\n  delete-old-images: true\n"+composeWithoutConfig)

	if !stub.called {
		t.Error("expected a deployment")
	}
}

func TestProcessSyncJob_ComposeConfig_KeepsChangesMadeWhileQueued(t *testing.T) {
	setupTestDB(t)
	app := seedSyncedApp(t)
	// The icon changes through the API after the job was queued.
	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).
		Updates(t.Context(), models.Application{Icon: "rocket"}); err != nil {
		t.Fatal(err)
	}

	runComposeSync(t, app, "x-orcacd:\n  delete-old-images: true\n"+composeWithoutConfig)

	if updated := loadApp(t, app.Id); updated.Icon != "rocket" || !updated.DeleteOldImages {
		t.Errorf("expected the icon from the API and the setting from x-orcacd, got %q and %v", updated.Icon, updated.DeleteOldImages)
	}
}

func TestProcessSyncJob_ComposeConfig_InvalidCommitFailsOncePerPoll(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t, seedRepo(t).Id, seedAgent(t).Id, composeWithoutConfig)
	repo, err := gorm.G[models.Repository](db.DB).Where("id = ?", app.RepositoryId).First(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	nop := zerolog.Nop()
	poll := func() {
		processSyncJob(t.Context(), syncJob{
			Application:        app,
			Repository:         repo,
			RepositoryProvider: &mockProvider{fileContent: "x-orcacd:\n  sync: auto\n" + composeWithoutConfig},
			Commit:             "badsha",
			Origin:             SyncOrigin{Source: models.ApplicationEventSourceRepositoryPolling},
		}, &nop)
	}

	poll()
	poll()

	if events := loadAppEvents(t, app.Id); len(events) != 1 {
		t.Errorf("expected one failed event for the invalid commit, got %d", len(events))
	}
}

func seedNotification(t *testing.T, name string, composeAssignable bool) models.Notification {
	t.Helper()
	notification := models.Notification{
		Name:              crypto.EncryptedString(name),
		Events:            models.NotificationEvents,
		Status:            models.NotificationStatusUnknown,
		Type:              models.NotificationTypeDiscord,
		Config:            crypto.EncryptedString("{}"),
		ComposeAssignable: composeAssignable,
	}
	if err := db.DB.Select("*").Create(&notification).Error; err != nil {
		t.Fatalf("seed notification: %v", err)
	}
	return notification
}

func appNotificationIds(t *testing.T, id string) []string {
	t.Helper()
	app, err := gorm.G[models.Application](db.DB).Preload("Notifications", nil).Where("id = ?", id).First(t.Context())
	if err != nil {
		t.Fatalf("load application: %v", err)
	}
	ids := make([]string, 0, len(app.Notifications))
	for _, notification := range app.Notifications {
		ids = append(ids, notification.Id)
	}
	slices.Sort(ids)
	return ids
}

func TestProcessSyncJob_ComposeConfig_Notifications(t *testing.T) {
	setupTestDB(t)
	ops := seedNotification(t, "ops", true)
	alerts := seedNotification(t, "alerts", true)
	previous := seedNotification(t, "previous", true)
	// An admin assigned the application to a notification that compose files
	// can't assign, which x-orcacd leaves alone.
	security := seedNotification(t, "security", false)
	app := seedApp(t, seedRepo(t).Id, seedAgent(t).Id, composeWithoutConfig)
	if err := db.DB.Model(&app).Association("Notifications").Replace([]models.Notification{previous, security}); err != nil {
		t.Fatal(err)
	}

	runComposeSync(t, app, "x-orcacd:\n  notifications: [ops, "+alerts.Id+"]\n"+composeWithoutConfig)

	want := []string{ops.Id, alerts.Id, security.Id}
	slices.Sort(want)
	if got := appNotificationIds(t, app.Id); !slices.Equal(got, want) {
		t.Errorf("notifications = %v, want %v", got, want)
	}

	// Without the key, the application leaves the notifications x-orcacd can
	// assign.
	runComposeSync(t, loadApp(t, app.Id), composeWithoutConfig)
	if got := appNotificationIds(t, app.Id); !slices.Equal(got, []string{security.Id}) {
		t.Errorf("notifications = %v, want only %s", got, security.Id)
	}
	if _, err := gorm.G[models.Notification](db.DB).Where("id = ?", ops.Id).First(t.Context()); err != nil {
		t.Errorf("expected the notification itself to stay: %v", err)
	}
}

func TestProcessSyncJob_ComposeConfig_UnknownOrAmbiguousNotification(t *testing.T) {
	setupTestDB(t)
	seedNotification(t, "ops", true)
	seedNotification(t, "ops", true)
	// A notification that doesn't allow compose files is never matched, so
	// its name can't be probed.
	private := seedNotification(t, "private", false)
	app := seedApp(t, seedRepo(t).Id, seedAgent(t).Id, composeWithoutConfig)

	stub := runComposeSync(t, app, "x-orcacd:\n  notifications: [ops, missing, private, "+private.Id+"]\n"+composeWithoutConfig)

	if stub.called {
		t.Error("expected no deployment")
	}
	updated := loadApp(t, app.Id)
	for _, want := range []string{
		`2 notifications are named "ops"`,
		`no notification "missing" allows compose files to assign applications`,
		`no notification "private" allows compose files to assign applications`,
		`no notification "` + private.Id + `" allows compose files to assign applications`,
	} {
		if updated.LastSyncError == nil || !strings.Contains(*updated.LastSyncError, want) {
			t.Errorf("expected the sync error to contain %q, got %v", want, updated.LastSyncError)
		}
	}
}

// A notification deleted between looking it up and saving is not created
// again from its partially loaded row.
func TestSaveNotifications_DeletedNotificationIsNotRecreated(t *testing.T) {
	setupTestDB(t)
	ops := seedNotification(t, "ops", true)
	app := seedApp(t, seedRepo(t).Id, seedAgent(t).Id, composeWithoutConfig)
	cfg, err := composeconfig.Parse("x-orcacd:\n  notifications: [ops]\n" + composeWithoutConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ResolveNotifications(t.Context()); err != nil {
		t.Fatal(err)
	}
	cfg.Apply(&app)
	if _, err := gorm.G[models.Notification](db.DB).Where("id = ?", ops.Id).Delete(t.Context()); err != nil {
		t.Fatal(err)
	}

	_ = cfg.SaveNotifications(t.Context(), &app)

	if count, _ := gorm.G[models.Notification](db.DB).Where("id = ?", ops.Id).Count(t.Context(), "*"); count != 0 {
		t.Error("expected the deleted notification not to be created again")
	}
}

func TestProcessSyncJob_ComposeConfig_ServiceSettingsDoNotDeploy(t *testing.T) {
	setupTestDB(t)
	app := seedSyncedApp(t, composeWithoutConfig)
	compose := "services:\n  app:\n    image: myimage:1.0\n    x-orcacd:\n      image-poll: false\n      health: false\n      release-age: 2h\n"

	stub := runComposeSync(t, app, compose)

	if stub.called {
		t.Error("expected no deployment when only x-orcacd of a service changed")
	}
	updated := loadApp(t, app.Id)
	if !slices.Equal(updated.ImagePollExcludedServices, []string{"app"}) || !slices.Equal(updated.HealthIgnoredServices, []string{"app"}) ||
		updated.ReleaseAgeServiceOverrides["app"] != 7200 {
		t.Errorf("expected the service settings to be saved, got %+v", updated)
	}
}
