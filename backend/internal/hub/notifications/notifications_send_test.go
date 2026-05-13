package notifications

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

func newNotificationLogger() *zerolog.Logger {
	var sink bytes.Buffer
	logger := zerolog.New(&sink)
	return &logger
}

func setNotificationConfig(t *testing.T, notificationId, rawConfig string) {
	t.Helper()

	rowsAffected, err := gorm.G[models.Notification](db.DB).
		Where("id = ?", notificationId).
		Update(t.Context(), "config", crypto.EncryptedString(rawConfig))
	if err != nil {
		t.Fatalf("failed to update notification config: %v", err)
	}
	if rowsAffected != 1 {
		t.Fatalf("expected one row to be updated, got %d", rowsAffected)
	}
}

func assertNotificationStatus(t *testing.T, notificationId string, want models.NotificationStatus) {
	t.Helper()

	notification, err := gorm.G[models.Notification](db.DB).Where("id = ?", notificationId).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load notification %s: %v", notificationId, err)
	}
	if notification.Status != want {
		t.Fatalf("expected notification status %q, got %q", want, notification.Status)
	}
}

func TestGetNotificationConfigApplicationNotFound(t *testing.T) {
	setupNotificationsTestDB(t)

	_, err := getNotificationConfig(context.Background(), "missing-application")
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected gorm.ErrRecordNotFound, got %v", err)
	}
}

func TestSendNotificationIgnoresEmptyMessage(t *testing.T) {
	setupNotificationsTestDB(t)

	app := seedNotificationTestApp(t, models.Healthy)
	notification := seedNotificationRecord(t, "empty-message", true, false, models.NotificationStatusUnknown, app.Id)

	SendNotification(app.Id, "   ", newNotificationLogger())

	assertNotificationStatus(t, notification.Id, models.NotificationStatusUnknown)
}

func TestSendNotificationApplicationNotFound(t *testing.T) {
	setupNotificationsTestDB(t)

	notification := seedNotificationRecord(t, "default", true, true, models.NotificationStatusUnknown)

	SendNotification("missing-application", "ping", newNotificationLogger())

	assertNotificationStatus(t, notification.Id, models.NotificationStatusUnknown)
}

func TestSendNotificationInvalidConfigMarksStatusError(t *testing.T) {
	setupNotificationsTestDB(t)

	app := seedNotificationTestApp(t, models.Healthy)
	notification := seedNotificationRecord(t, "invalid-config", true, false, models.NotificationStatusUnknown, app.Id)
	setNotificationConfig(t, notification.Id, `{"webhookId":"123456789"}`)

	SendNotification(app.Id, "deploy done", newNotificationLogger())

	assertNotificationStatus(t, notification.Id, models.NotificationStatusError)
}

func TestSendNotificationCreateSenderErrorMarksStatusError(t *testing.T) {
	setupNotificationsTestDB(t)

	app := seedNotificationTestApp(t, models.Healthy)
	notification := seedNotificationRecord(t, "create-sender-error", true, false, models.NotificationStatusUnknown, app.Id)
	setNotificationConfig(t, notification.Id, "not-a-url")

	SendNotification(app.Id, "deploy done", newNotificationLogger())

	assertNotificationStatus(t, notification.Id, models.NotificationStatusError)
}

func TestSendNotificationSuccessMarksStatusSuccess(t *testing.T) {
	setupNotificationsTestDB(t)

	app := seedNotificationTestApp(t, models.Healthy)
	notification := seedNotificationRecord(t, "send-success", true, false, models.NotificationStatusUnknown, app.Id)
	setNotificationConfig(t, notification.Id, "stdout://")

	SendNotification(app.Id, "deploy done", newNotificationLogger())

	assertNotificationStatus(t, notification.Id, models.NotificationStatusSuccess)
}

func TestSendNotificationDispatchErrorMarksStatusError(t *testing.T) {
	setupNotificationsTestDB(t)

	app := seedNotificationTestApp(t, models.Healthy)
	notification := seedNotificationRecord(t, "send-error", true, false, models.NotificationStatusUnknown, app.Id)
	setNotificationConfig(t, notification.Id, "generic+http://127.0.0.1:1")

	SendNotification(app.Id, "deploy done", newNotificationLogger())

	assertNotificationStatus(t, notification.Id, models.NotificationStatusError)
}

func TestSendTestNotificationInvalidConfig(t *testing.T) {
	err := SendTestNotification(models.NotificationTypeDiscord, `{"webhookId":"123456789"}`, "ping")
	if !errors.Is(err, ErrInvalidNotificationConfig) {
		t.Fatalf("expected ErrInvalidNotificationConfig, got %v", err)
	}
}

func TestSendTestNotificationCreateSenderError(t *testing.T) {
	err := SendTestNotification(models.NotificationTypeDiscord, "not-a-url", "ping")
	if !errors.Is(err, ErrInvalidNotificationConfig) {
		t.Fatalf("expected ErrInvalidNotificationConfig, got %v", err)
	}
}

func TestSendTestNotificationSuccessWithDefaultMessage(t *testing.T) {
	err := SendTestNotification(models.NotificationTypeDiscord, "stdout://", "   ")
	if err != nil {
		t.Fatalf("expected successful test notification, got %v", err)
	}
}

func TestSendTestNotificationDispatchError(t *testing.T) {
	err := SendTestNotification(models.NotificationTypeDiscord, "generic+http://127.0.0.1:1", "ping")
	if !errors.Is(err, ErrNotificationDispatch) {
		t.Fatalf("expected ErrNotificationDispatch, got %v", err)
	}
}

func TestSetNotificationStatus(t *testing.T) {
	setupNotificationsTestDB(t)

	app := seedNotificationTestApp(t, models.Healthy)
	notification := seedNotificationRecord(t, "set-status", true, false, models.NotificationStatusUnknown, app.Id)

	setNotificationStatus(notification.Id, models.NotificationStatusSuccess, newNotificationLogger())

	assertNotificationStatus(t, notification.Id, models.NotificationStatusSuccess)
}

func TestSetNotificationStatusNotificationNotFound(t *testing.T) {
	setupNotificationsTestDB(t)

	notification := seedNotificationRecord(t, "missing-status", true, true, models.NotificationStatusUnknown)

	setNotificationStatus("missing-notification", models.NotificationStatusError, newNotificationLogger())

	assertNotificationStatus(t, notification.Id, models.NotificationStatusUnknown)
}