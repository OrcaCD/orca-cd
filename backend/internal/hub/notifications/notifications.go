package notifications

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/nicholas-fedor/shoutrrr"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

const notificationQueryTimeout = 10 * time.Second

const DefaultTestNotificationMessage = "This is a test notification from OrcaCD."

var (
	ErrInvalidNotificationConfig = errors.New("invalid notification config")
	ErrNotificationDispatch      = errors.New("notification dispatch failed")
)

func SendNotification(applicationId string, message string, log *zerolog.Logger) {
	if strings.TrimSpace(message) == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), notificationQueryTimeout)
	defer cancel()

	configs, err := getNotificationConfig(ctx, applicationId)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Warn().Str("applicationId", applicationId).Msg("application not found while sending notifications")
			return
		}
		log.Error().Err(err).Str("applicationId", applicationId).Msg("failed to load notification config")
		return
	}

	for i := range configs {
		targets, parseErr := BuildShouterrrUrls(configs[i].Type, configs[i].Config.String())
		if parseErr != nil {
			log.Error().
				Err(parseErr).
				Str("applicationId", applicationId).
				Str("notificationId", configs[i].Id).
				Msg("failed to parse notification config")
			continue
		}

		sender, createErr := shoutrrr.CreateSender(targets...)
		if createErr != nil {
			log.Error().
				Err(createErr).
				Str("applicationId", applicationId).
				Str("notificationId", configs[i].Id).
				Msg("failed to create notification sender")
			continue
		}

		sendErrs := sender.Send(message, nil)
		for _, sendErr := range sendErrs {
			if sendErr == nil {
				continue
			}
			log.Error().
				Err(sendErr).
				Str("applicationId", applicationId).
				Str("notificationId", configs[i].Id).
				Msg("failed to send notification")
		}
	}
}

func getNotificationConfig(ctx context.Context, applicationId string) ([]models.Notification, error) {
	app, err := gorm.G[models.Application](db.DB).
		Select("id", "health_status").
		Where("id = ?", applicationId).
		First(ctx)
	if err != nil {
		return nil, err
	}

	healthStatus := models.NotificationStatus(app.HealthStatus)
	allowedStatuses := []models.NotificationStatus{models.NotificationUnknownHealth, healthStatus}

	var notifications []models.Notification
	err = db.DB.WithContext(ctx).
		Table("notifications").
		Select("notifications.*").
		Joins("LEFT JOIN application_notifications ON application_notifications.notification_id = notifications.id").
		Where("notifications.enabled = ?", true).
		Where("(notifications.enable_by_default = ? OR application_notifications.application_id = ?)", true, applicationId).
		Where("notifications.status IN ?", allowedStatuses).
		Group("notifications.id").
		Find(&notifications).Error
	if err != nil {
		return nil, err
	}

	return notifications, nil
}

func SendTestNotification(notificationType models.NotificationType, rawConfig, message string) error {
	targets, err := BuildShouterrrUrls(notificationType, rawConfig)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidNotificationConfig, err)
	}

	sender, err := shoutrrr.CreateSender(targets...)
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
