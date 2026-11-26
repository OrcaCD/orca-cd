package composeconfig

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func setupNotificationsDB(t *testing.T) {
	t.Helper()
	testDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "notifications.db")), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := testDB.AutoMigrate(&models.Agent{}, &models.Repository{}, &models.Application{}, &models.Notification{}); err != nil {
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
}

func createNotification(t *testing.T, name string, composeAssignable bool) models.Notification {
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
		t.Fatalf("create notification: %v", err)
	}
	return notification
}

func createApplication(t *testing.T) models.Application {
	t.Helper()
	app := models.Application{
		Name:                crypto.EncryptedString("app"),
		SyncStatus:          models.Synced,
		HealthStatus:        models.Healthy,
		ComposeFile:         crypto.EncryptedString(""),
		PreviousComposeFile: crypto.EncryptedString(""),
	}
	if err := db.DB.Create(&app).Error; err != nil {
		t.Fatalf("create application: %v", err)
	}
	return app
}

func linkedNotifications(t *testing.T, app *models.Application) []string {
	t.Helper()
	var notifications []models.Notification
	if err := db.DB.Model(app).Association("Notifications").Find(&notifications); err != nil {
		t.Fatalf("load notifications: %v", err)
	}
	ids := make([]string, 0, len(notifications))
	for _, notification := range notifications {
		ids = append(ids, notification.Id)
	}
	slices.Sort(ids)
	return ids
}

// apply parses compose, resolves its notifications and saves them for app.
func apply(t *testing.T, app *models.Application, compose string) error {
	t.Helper()
	cfg := mustParse(t, compose)
	if err := cfg.ResolveNotifications(t.Context()); err != nil {
		return err
	}
	cfg.Apply(app)
	return cfg.SaveNotifications(t.Context(), app)
}

func sorted(ids ...string) []string {
	slices.Sort(ids)
	return ids
}

func TestNotifications_ManagesOnlyAssignableLinks(t *testing.T) {
	setupNotificationsDB(t)
	ops := createNotification(t, "ops", true)
	alerts := createNotification(t, "alerts", true)
	previous := createNotification(t, "previous", true)
	security := createNotification(t, "security", false)
	app := createApplication(t)
	if err := db.DB.Model(&app).Association("Notifications").Replace([]models.Notification{previous, security}); err != nil {
		t.Fatal(err)
	}

	if err := apply(t, &app, "x-orcacd:\n  notifications: [ops, "+alerts.Id+", ops]\n"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got, want := linkedNotifications(t, &app), sorted(ops.Id, alerts.Id, security.Id); !slices.Equal(got, want) {
		t.Errorf("notifications = %v, want %v", got, want)
	}

	// Saving the same list again changes nothing.
	if err := apply(t, &app, "x-orcacd:\n  notifications: [ops, alerts]\n"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got, want := linkedNotifications(t, &app), sorted(ops.Id, alerts.Id, security.Id); !slices.Equal(got, want) {
		t.Errorf("notifications = %v, want %v", got, want)
	}

	// Without the key, only the links to assignable notifications go.
	if err := apply(t, &app, "services: {}\n"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := linkedNotifications(t, &app); !slices.Equal(got, []string{security.Id}) {
		t.Errorf("notifications = %v, want only %s", got, security.Id)
	}

	// Notifications set through the API stay when x-orcacd never set them.
	if err := apply(t, &app, "services: {}\n"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := linkedNotifications(t, &app); !slices.Equal(got, []string{security.Id}) {
		t.Errorf("notifications = %v, want only %s", got, security.Id)
	}
}

func TestResolveNotifications_Errors(t *testing.T) {
	setupNotificationsDB(t)
	createNotification(t, "ops", true)
	createNotification(t, "ops", true)
	private := createNotification(t, "private", false)

	cfg := mustParse(t, "x-orcacd:\n  notifications: [ops, missing, private, "+private.Id+"]\n")
	err := cfg.ResolveNotifications(t.Context())
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{
		`x-orcacd.notifications: 2 notifications are named "ops", use the ID of one`,
		`x-orcacd.notifications: no notification "missing" allows compose files to assign applications`,
		// A notification that doesn't allow it gives the same error, so its
		// name can't be probed.
		`x-orcacd.notifications: no notification "private" allows compose files to assign applications`,
		`no notification "` + private.Id + `" allows compose files to assign applications`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}

func TestResolveNotifications_WithoutKeyDoesNothing(t *testing.T) {
	// No database is needed when x-orcacd doesn't list notifications.
	cfg := mustParse(t, "services: {}\n")
	if err := cfg.ResolveNotifications(t.Context()); err != nil {
		t.Errorf("ResolveNotifications() = %v", err)
	}
	app := defaultApplication()
	if err := cfg.SaveNotifications(t.Context(), &app); err != nil {
		t.Errorf("SaveNotifications() = %v", err)
	}
}
