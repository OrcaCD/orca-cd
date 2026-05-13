package routes

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	hubnotifications "github.com/OrcaCD/orca-cd/internal/hub/notifications"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/gin-gonic/gin"
	"github.com/nicholas-fedor/shoutrrr"
	"gorm.io/gorm"
)

const NotificationsPath = "/api/v1/notifications"

type createNotificationRequest struct {
	Name            string                  `json:"name" binding:"required,min=1,max=128"`
	Enabled         *bool                   `json:"enabled"`
	EnableByDefault *bool                   `json:"enableByDefault"`
	Type            models.NotificationType `json:"type" binding:"required"`
	Config          json.RawMessage         `json:"config" binding:"required"`
	ApplicationIds  []string                `json:"applicationIds"`
}

type updateNotificationRequest struct {
	Name            string                  `json:"name" binding:"required,min=1,max=128"`
	Enabled         *bool                   `json:"enabled"`
	EnableByDefault *bool                   `json:"enableByDefault"`
	Type            models.NotificationType `json:"type" binding:"required"`
	Config          json.RawMessage         `json:"config" binding:"required"`
	ApplicationIds  []string                `json:"applicationIds"`
}

type testNotificationRequest struct {
	Message string `json:"message"`
}

var sendTestNotification = hubnotifications.SendTestNotification

type notificationResponse struct {
	Id              string   `json:"id"`
	Name            string   `json:"name"`
	Enabled         bool     `json:"enabled"`
	EnableByDefault bool     `json:"enableByDefault"`
	Status          string   `json:"status"`
	Type            string   `json:"type"`
	Config          *string  `json:"config,omitempty"`
	ApplicationIds  []string `json:"applicationIds"`
	CreatedAt       string   `json:"createdAt"`
	UpdatedAt       string   `json:"updatedAt"`
}

func ListNotificationsHandler(c *gin.Context) {
	includeConfig, err := parseIncludeConfigQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	items, err := gorm.G[models.Notification](db.DB).
		Preload("Applications", nil).
		Order("created_at ASC").
		Find(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	response := make([]notificationResponse, 0, len(items))
	for i := range items {
		response = append(response, toNotificationResponse(&items[i], includeConfig))
	}

	c.JSON(http.StatusOK, response)
}

func CreateNotificationHandler(c *gin.Context) {
	var req createNotificationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: name, type and config are required"})
		return
	}

	normalizedName, err := normalizeNotificationName(req.Name)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if !isValidNotificationType(req.Type) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid type"})
		return
	}

	normalizedConfig, err := normalizeNotificationConfig(req.Config)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateNotificationConfig(req.Type, normalizedConfig); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid config: " + err.Error()})
		return
	}

	ctx := c.Request.Context()
	applications, missingApplicationId, err := loadNotificationApplications(ctx, req.ApplicationIds)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if missingApplicationId != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "application not found: " + missingApplicationId})
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	enableByDefault := false
	if req.EnableByDefault != nil {
		enableByDefault = *req.EnableByDefault
	}

	notification := models.Notification{
		Name:            crypto.EncryptedString(normalizedName),
		Enabled:         enabled,
		EnableByDefault: enableByDefault,
		Status:          models.NotificationStatusUnknown,
		Type:            req.Type,
		Config:          crypto.EncryptedString(normalizedConfig),
	}

	err = db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := gorm.G[models.Notification](tx).Select("*").Create(ctx, &notification); err != nil {
			return err
		}

		if err := tx.Model(&notification).Association("Applications").Replace(applications); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	createdNotification, err := getNotificationById(ctx, notification.Id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusCreated, toNotificationResponse(&createdNotification, true))
	sse.PublishUpdate(NotificationsPath)
}

func UpdateNotificationHandler(c *gin.Context) {
	id := c.Param("id")

	var req updateNotificationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: name, type and config are required"})
		return
	}

	normalizedName, err := normalizeNotificationName(req.Name)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if !isValidNotificationType(req.Type) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid type"})
		return
	}

	normalizedConfig, err := normalizeNotificationConfig(req.Config)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateNotificationConfig(req.Type, normalizedConfig); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid config: " + err.Error()})
		return
	}

	ctx := c.Request.Context()

	existingNotification, err := getNotificationById(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "notification not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	applications, missingApplicationId, err := loadNotificationApplications(ctx, req.ApplicationIds)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if missingApplicationId != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "application not found: " + missingApplicationId})
		return
	}

	enabled := existingNotification.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	enableByDefault := existingNotification.EnableByDefault
	if req.EnableByDefault != nil {
		enableByDefault = *req.EnableByDefault
	}

	updates := models.Notification{
		Name:            crypto.EncryptedString(normalizedName),
		Enabled:         enabled,
		EnableByDefault: enableByDefault,
		Status:          models.NotificationStatusUnknown,
		Type:            req.Type,
		Config:          crypto.EncryptedString(normalizedConfig),
	}

	err = db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rowsAffected, err := gorm.G[models.Notification](tx).
			Where("id = ?", id).
			Select("name", "enabled", "enable_by_default", "status", "type", "config").
			Updates(ctx, updates)
		if err != nil {
			return err
		}
		if rowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}

		if err := tx.Model(&existingNotification).Association("Applications").Replace(applications); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "notification not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	updatedNotification, err := getNotificationById(ctx, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, toNotificationResponse(&updatedNotification, true))
	sse.PublishUpdate(NotificationsPath)
}

func DeleteNotificationHandler(c *gin.Context) {
	id := c.Param("id")

	rowsAffected, err := gorm.G[models.Notification](db.DB).Where("id = ?", id).Delete(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "notification not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "notification deleted"})
	sse.PublishUpdate(NotificationsPath)
}

func TestNotificationHandler(c *gin.Context) {
	id := c.Param("id")
	ctx := c.Request.Context()

	notification, err := gorm.G[models.Notification](db.DB).Where("id = ?", id).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "notification not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	var req testNotificationRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: message must be a string"})
		return
	}

	sendErr := sendTestNotification(notification.Type, notification.Config.String(), req.Message)
	status := models.NotificationStatusSuccess
	if sendErr != nil {
		status = models.NotificationStatusError
	}

	if err := updateNotificationStatus(ctx, notification.Id, status); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "notification not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	sse.PublishUpdate(NotificationsPath)

	if sendErr != nil {
		switch {
		case errors.Is(sendErr, hubnotifications.ErrInvalidNotificationConfig):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid config: " + sendErr.Error()})
		case errors.Is(sendErr, hubnotifications.ErrNotificationDispatch):
			c.JSON(http.StatusBadGateway, gin.H{"error": "failed to send test notification"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "test notification sent"})
}

func updateNotificationStatus(ctx context.Context, id string, status models.NotificationStatus) error {
	rowsAffected, err := gorm.G[models.Notification](db.DB).
		Where("id = ?", id).
		Update(ctx, "status", status)
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}

	return nil
}

func getNotificationById(ctx context.Context, id string) (models.Notification, error) {
	return gorm.G[models.Notification](db.DB).
		Preload("Applications", nil).
		Where("id = ?", id).
		First(ctx)
}

func toNotificationResponse(notification *models.Notification, includeConfig bool) notificationResponse {
	applicationIds := make([]string, 0, len(notification.Applications))
	for i := range notification.Applications {
		applicationIds = append(applicationIds, notification.Applications[i].Id)
	}
	sort.Strings(applicationIds)

	response := notificationResponse{
		Id:              notification.Id,
		Name:            notification.Name.String(),
		Enabled:         notification.Enabled,
		EnableByDefault: notification.EnableByDefault,
		Status:          string(notification.Status),
		Type:            string(notification.Type),
		ApplicationIds:  applicationIds,
		CreatedAt:       notification.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       notification.UpdatedAt.Format(time.RFC3339),
	}

	if includeConfig {
		config := notification.Config.String()
		response.Config = &config
	}

	return response
}

func isValidNotificationType(notificationType models.NotificationType) bool {
	_, err := hubnotifications.Get(notificationType)
	return err == nil
}

func parseIncludeConfigQuery(c *gin.Context) (bool, error) {
	raw := strings.TrimSpace(c.Query("includeConfig"))
	if raw == "" {
		return false, nil
	}

	includeConfig, err := strconv.ParseBool(raw)
	if err != nil {
		return false, errors.New("invalid includeConfig: must be a boolean")
	}

	return includeConfig, nil
}

func normalizeNotificationName(rawName string) (string, error) {
	trimmedName := strings.TrimSpace(rawName)
	if trimmedName == "" {
		return "", errors.New("invalid name: must not be empty")
	}

	if utf8.RuneCountInString(trimmedName) > 128 {
		return "", errors.New("invalid name: must be at most 128 characters")
	}

	return trimmedName, nil
}

func validateNotificationConfig(notificationType models.NotificationType, rawConfig string) error {
	targets, err := hubnotifications.BuildShouterrrUrls(notificationType, rawConfig)
	if err != nil {
		return err
	}

	if _, err := shoutrrr.CreateSender(targets...); err != nil {
		return err
	}

	return nil
}

func normalizeNotificationConfig(raw json.RawMessage) (string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return "", errors.New("invalid config: must be a non-empty string or JSON object")
	}

	if strings.HasPrefix(trimmed, "\"") {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", errors.New("invalid config: expected a valid JSON string")
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return "", errors.New("invalid config: must not be empty")
		}
		return value, nil
	}

	if !json.Valid([]byte(trimmed)) {
		return "", errors.New("invalid config: expected valid JSON")
	}

	return trimmed, nil
}

func normalizeNotificationApplicationIds(applicationIds []string) []string {
	if len(applicationIds) == 0 {
		return nil
	}

	normalized := make([]string, 0, len(applicationIds))
	seen := make(map[string]struct{}, len(applicationIds))

	for i := range applicationIds {
		id := strings.TrimSpace(applicationIds[i])
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}

	return normalized
}

func loadNotificationApplications(ctx context.Context, applicationIds []string) ([]models.Application, string, error) {
	normalizedIds := normalizeNotificationApplicationIds(applicationIds)
	if len(normalizedIds) == 0 {
		return []models.Application{}, "", nil
	}

	applications, err := gorm.G[models.Application](db.DB).Where("id IN ?", normalizedIds).Find(ctx)
	if err != nil {
		return nil, "", err
	}
	if len(applications) != len(normalizedIds) {
		foundById := make(map[string]struct{}, len(applications))
		for i := range applications {
			foundById[applications[i].Id] = struct{}{}
		}
		for i := range normalizedIds {
			if _, ok := foundById[normalizedIds[i]]; !ok {
				return nil, normalizedIds[i], nil
			}
		}
	}

	return applications, "", nil
}
