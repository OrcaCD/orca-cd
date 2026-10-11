package notifications

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/notifications/provider"
	"github.com/OrcaCD/orca-cd/internal/shared/httpclient"
	"github.com/nicholas-fedor/shoutrrr"
	"github.com/nicholas-fedor/shoutrrr/pkg/types"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

const notificationQueryTimeout = 10 * time.Second

const DefaultTestNotificationMessage = "This is a test notification from OrcaCD."

// notificationHTTPClient is the HTTP client used by shoutrrr for all outbound
// notification requests.
var notificationHTTPClient = httpclient.Default

var (
	ErrInvalidNotificationConfig = errors.New("invalid notification config")
	ErrNotificationDispatch      = errors.New("notification dispatch failed")
)

// notificationScope describes how notifications are assigned to one resource
// type: explicitly through a join table or implicitly through an "all" flag.
type notificationScope struct {
	logKey     string
	allColumn  string
	joinTable  string
	foreignKey string
	exists     func(ctx context.Context, id string) error
}

var (
	applicationScope = notificationScope{
		logKey:     "applicationId",
		allColumn:  "all_applications",
		joinTable:  "application_notifications",
		foreignKey: "application_id",
		exists:     resourceExists[models.Application],
	}
	agentScope = notificationScope{
		logKey:     "agentId",
		allColumn:  "all_agents",
		joinTable:  "agent_notifications",
		foreignKey: "agent_id",
		exists:     resourceExists[models.Agent],
	}
	repositoryScope = notificationScope{
		logKey:     "repositoryId",
		allColumn:  "all_repositories",
		joinTable:  "repository_notifications",
		foreignKey: "repository_id",
		exists:     resourceExists[models.Repository],
	}
)

func resourceExists[T any](ctx context.Context, id string) error {
	_, err := gorm.G[T](db.DB).Select("id").Where("id = ?", id).First(ctx)
	return err
}

func SendForApplication(applicationId string, event models.NotificationEvent, message string, log *zerolog.Logger) {
	notify(applicationScope, applicationId, event, message, log)
}

func SendForAgent(agentId string, event models.NotificationEvent, message string, log *zerolog.Logger) {
	notify(agentScope, agentId, event, message, log)
}

func SendForRepository(repositoryId string, event models.NotificationEvent, message string, log *zerolog.Logger) {
	notify(repositoryScope, repositoryId, event, message, log)
}

func notify(scope notificationScope, resourceId string, event models.NotificationEvent, message string, log *zerolog.Logger) {
	if strings.TrimSpace(message) == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), notificationQueryTimeout)
	defer cancel()

	configs, err := getNotificationConfig(ctx, scope, resourceId, event)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Warn().Str(scope.logKey, resourceId).Msg("resource not found while sending notifications")
			return
		}
		log.Error().Err(err).Str(scope.logKey, resourceId).Msg("failed to load notification config")
		return
	}

	for i := range configs {
		status := models.NotificationStatusSuccess
		if err := dispatch(&configs[i], message); err != nil {
			log.Error().
				Err(err).
				Str(scope.logKey, resourceId).
				Str("notificationId", configs[i].Id).
				Str("event", string(event)).
				Msg("failed to send notification")
			status = models.NotificationStatusError
		}
		setNotificationStatus(configs[i].Id, status, log)
	}
}

func dispatch(notification *models.Notification, message string) error {
	targets, err := provider.BuildShoutrrrUrls(notification.Type, notification.Config.String())
	if err != nil {
		return fmt.Errorf("parse notification config: %w", err)
	}

	sender, err := shoutrrr.CreateSenderWithOptions(types.SenderOptions{HTTPClient: notificationHTTPClient}, targets...)
	if err != nil {
		return fmt.Errorf("create notification sender: %w", err)
	}

	return errors.Join(sender.Send(message, nil)...)
}

// getNotificationConfig returns the enabled notifications that cover the
// resource, either explicitly or through the scope's "all" flag, and subscribe
// to the event.
func getNotificationConfig(ctx context.Context, scope notificationScope, resourceId string, event models.NotificationEvent) ([]models.Notification, error) {
	if err := scope.exists(ctx, resourceId); err != nil {
		return nil, err
	}

	notifications, err := gorm.G[models.Notification](db.DB).
		Where("enabled = ?", true).
		Where(
			"("+scope.allColumn+" = ? OR id IN (SELECT notification_id FROM "+scope.joinTable+" WHERE "+scope.foreignKey+" = ?))",
			true, resourceId,
		).
		Find(ctx)
	if err != nil {
		return nil, err
	}

	return slices.DeleteFunc(notifications, func(n models.Notification) bool {
		return !n.SubscribesTo(event)
	}), nil
}

func SendTestNotification(notificationType models.NotificationType, rawConfig, message string) error {
	targets, err := provider.BuildShoutrrrUrls(notificationType, rawConfig)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidNotificationConfig, err)
	}

	sender, err := shoutrrr.CreateSenderWithOptions(types.SenderOptions{HTTPClient: notificationHTTPClient}, targets...)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidNotificationConfig, err)
	}

	msg := strings.TrimSpace(message)
	if msg == "" {
		msg = DefaultTestNotificationMessage
	}

	sendErrs := sender.Send(msg, nil)
	errList := make([]error, 0, len(sendErrs))
	for i := range sendErrs {
		if sendErrs[i] != nil {
			errList = append(errList, sendErrs[i])
		}
	}

	if len(errList) > 0 {
		return fmt.Errorf("%w: %w", ErrNotificationDispatch, errors.Join(errList...))
	}

	return nil
}

func setNotificationStatus(notificationId string, status models.NotificationStatus, log *zerolog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), notificationQueryTimeout)
	defer cancel()

	rowsAffected, err := gorm.G[models.Notification](db.DB).
		Where("id = ?", notificationId).
		Update(ctx, "status", status)
	if err != nil {
		log.Error().Err(err).Str("notificationId", notificationId).Str("status", string(status)).Msg("failed to update notification status")
		return
	}
	if rowsAffected == 0 {
		log.Warn().Str("notificationId", notificationId).Msg("notification not found while updating status")
	}
}
