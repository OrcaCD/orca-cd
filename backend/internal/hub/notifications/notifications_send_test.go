package notifications

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/notifications/provider"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

const testHTTPNotificationType models.NotificationType = "test-http"

type passthroughNotificationProvider struct{}

func (passthroughNotificationProvider) BuildShoutrrrUrls(rawConfig string) ([]string, error) {
	return []string{rawConfig}, nil
}

type capturedNotificationRequest struct {
	Method      string
	ContentType string
	Body        string
}

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

func setNotificationTypeAndConfig(t *testing.T, notificationId string, notificationType models.NotificationType, rawConfig string) {
	t.Helper()

	rowsAffected, err := gorm.G[models.Notification](db.DB).
		Where("id = ?", notificationId).
		Updates(t.Context(), models.Notification{
			Type:   notificationType,
			Config: crypto.EncryptedString(rawConfig),
		})
	if err != nil {
		t.Fatalf("failed to update notification type and config: %v", err)
	}
	if rowsAffected != 1 {
		t.Fatalf("expected one row to be updated, got %d", rowsAffected)
	}
}

func registerTestHTTPNotificationProvider(t *testing.T) {
	t.Helper()

	provider.Register(testHTTPNotificationType, passthroughNotificationProvider{})
}

func genericNotificationURL(t *testing.T, serverURL string) string {
	t.Helper()

	parsed, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("failed to parse test server URL %q: %v", serverURL, err)
	}
	parsed.Scheme = "generic"
	query := parsed.Query()
	query.Set("disabletls", "yes")
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func newNotificationCaptureServer(t *testing.T, statusCode int) (*httptest.Server, <-chan capturedNotificationRequest) {
	t.Helper()

	requests := make(chan capturedNotificationRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read notification request body: %v", err)
		}

		requests <- capturedNotificationRequest{
			Method:      r.Method,
			ContentType: r.Header.Get("Content-Type"),
			Body:        string(body),
		}

		w.WriteHeader(statusCode)
	}))
	t.Cleanup(server.Close)

	return server, requests
}

func assertCapturedNotificationRequest(t *testing.T, requests <-chan capturedNotificationRequest, wantBody string) {
	t.Helper()

	select {
	case request := <-requests:
		if request.Method != http.MethodPost {
			t.Fatalf("expected notification request method %q, got %q", http.MethodPost, request.Method)
		}
		if !strings.HasPrefix(request.ContentType, "text/plain") {
			t.Fatalf("expected text/plain content type, got %q", request.ContentType)
		}
		if request.Body != wantBody {
			t.Fatalf("expected notification body %q, got %q", wantBody, request.Body)
		}
	default:
		t.Fatal("expected HTTP server to receive notification request")
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

func TestSendNotificationPostsToHTTPWebhook(t *testing.T) {
	setupNotificationsTestDB(t)
	registerTestHTTPNotificationProvider(t)
	server, requests := newNotificationCaptureServer(t, http.StatusNoContent)

	app := seedNotificationTestApp(t, models.Healthy)
	notification := seedNotificationRecord(t, "http-webhook", true, false, models.NotificationStatusUnknown, app.Id)
	setNotificationTypeAndConfig(t, notification.Id, testHTTPNotificationType, genericNotificationURL(t, server.URL))

	SendNotification(app.Id, "deploy done", newNotificationLogger())

	assertCapturedNotificationRequest(t, requests, "deploy done")
	assertNotificationStatus(t, notification.Id, models.NotificationStatusSuccess)
}

func TestSendNotificationHTTPWebhookErrorMarksStatusError(t *testing.T) {
	setupNotificationsTestDB(t)
	registerTestHTTPNotificationProvider(t)
	server, requests := newNotificationCaptureServer(t, http.StatusInternalServerError)

	app := seedNotificationTestApp(t, models.Healthy)
	notification := seedNotificationRecord(t, "http-webhook-error", true, false, models.NotificationStatusUnknown, app.Id)
	setNotificationTypeAndConfig(t, notification.Id, testHTTPNotificationType, genericNotificationURL(t, server.URL))

	SendNotification(app.Id, "deploy failed", newNotificationLogger())

	assertCapturedNotificationRequest(t, requests, "deploy failed")
	assertNotificationStatus(t, notification.Id, models.NotificationStatusError)
}

func TestSendTestNotificationPostsToHTTPWebhook(t *testing.T) {
	registerTestHTTPNotificationProvider(t)
	server, requests := newNotificationCaptureServer(t, http.StatusNoContent)

	err := SendTestNotification(testHTTPNotificationType, genericNotificationURL(t, server.URL), "ping")
	if err != nil {
		t.Fatalf("SendTestNotification() error = %v", err)
	}

	assertCapturedNotificationRequest(t, requests, "ping")
}

func TestSendTestNotificationInvalidConfig(t *testing.T) {
	err := SendTestNotification(models.NotificationTypeDiscord, `{"webhookId":"123456789"}`, "ping")
	if !errors.Is(err, ErrInvalidNotificationConfig) {
		t.Fatalf("expected ErrInvalidNotificationConfig, got %v", err)
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
