package routes

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	hubnotifications "github.com/OrcaCD/orca-cd/internal/hub/notifications"
	"github.com/OrcaCD/orca-cd/internal/hub/notifications/provider"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/OrcaCD/orca-cd/internal/shared/logger"
	"github.com/gin-gonic/gin"
	"github.com/nicholas-fedor/shoutrrr"
	"github.com/nicholas-fedor/shoutrrr/pkg/types"
	"gorm.io/gorm"
)

type createNotificationRequest struct {
	Name            string                     `json:"name" binding:"required,min=1,max=128"`
	Enabled         *bool                      `json:"enabled"`
	AllApplications *bool                      `json:"allApplications"`
	AllAgents       *bool                      `json:"allAgents"`
	AllRepositories *bool                      `json:"allRepositories"`
	Events          []models.NotificationEvent `json:"events"`
	Type            models.NotificationType    `json:"type" binding:"required"`
	Config          json.RawMessage            `json:"config" binding:"required"`
	ApplicationIds  []string                   `json:"applicationIds"`
	AgentIds        []string                   `json:"agentIds"`
	RepositoryIds   []string                   `json:"repositoryIds"`
}

type setNotificationEnabledRequest struct {
	Enabled *bool `json:"enabled"`
}

type updateNotificationRequest struct {
	Enabled         *bool                      `json:"enabled"`
	AllApplications *bool                      `json:"allApplications"`
	AllAgents       *bool                      `json:"allAgents"`
	AllRepositories *bool                      `json:"allRepositories"`
	Events          []models.NotificationEvent `json:"events"`
	ApplicationIds  []string                   `json:"applicationIds"`
	AgentIds        []string                   `json:"agentIds"`
	RepositoryIds   []string                   `json:"repositoryIds"`
}

type testNotificationRequest struct {
	Message string `json:"message"`
}

var sendTestNotification = hubnotifications.SendTestNotification

var notificationsLog = logger.New("notifications", false)

type notificationResponse struct {
	Id              string                     `json:"id"`
	Name            string                     `json:"name"`
	Enabled         bool                       `json:"enabled"`
	AllApplications bool                       `json:"allApplications"`
	AllAgents       bool                       `json:"allAgents"`
	AllRepositories bool                       `json:"allRepositories"`
	Events          []models.NotificationEvent `json:"events"`
	Status          string                     `json:"status"`
	Type            string                     `json:"type"`
	ApplicationIds  []string                   `json:"applicationIds"`
	AgentIds        []string                   `json:"agentIds"`
	RepositoryIds   []string                   `json:"repositoryIds"`
	CreatedAt       string                     `json:"createdAt"`
	UpdatedAt       string                     `json:"updatedAt"`
}

func ListNotificationsHandler(c *gin.Context) {
	perms, ok := currentPermissions(c)
	if !ok {
		return
	}

	items, err := gorm.G[models.Notification](db.DB).
		Scopes(perms.Scope(models.ResourceNotification, models.ActionRead, "id")).
		Preload("Applications", selectNotificationResourceId).
		Preload("Agents", selectNotificationResourceId).
		Preload("Repositories", selectNotificationResourceId).
		Order("created_at ASC").
		Find(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	response := make([]notificationResponse, 0, len(items))
	for i := range items {
		response = append(response, toNotificationResponse(perms, &items[i]))
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

	// Omitting events subscribes to everything, matching the behavior before
	// events became selectable.
	events := slices.Clone(models.NotificationEvents)
	if req.Events != nil {
		events, err = normalizeNotificationEvents(req.Events)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	allApplications := req.AllApplications != nil && *req.AllApplications
	allAgents := req.AllAgents != nil && *req.AllAgents
	allRepositories := req.AllRepositories != nil && *req.AllRepositories

	ctx := c.Request.Context()
	perms, ok := currentPermissions(c)
	if !ok {
		return
	}
	applicationIds, agentIds, repositoryIds, ok := authorizeNotificationTargets(c, perms, nil, notificationScope{
		allApplications: allApplications, allAgents: allAgents, allRepositories: allRepositories,
		applicationIds: req.ApplicationIds, agentIds: req.AgentIds, repositoryIds: req.RepositoryIds,
	})
	if !ok {
		return
	}

	targets, ok := loadNotificationTargets(c,
		scopedNotificationResourceIds(allApplications, applicationIds),
		scopedNotificationResourceIds(allAgents, agentIds),
		scopedNotificationResourceIds(allRepositories, repositoryIds),
	)
	if !ok {
		return
	}

	// Reject configs that can't deliver before saving them, so new
	// notifications never start in an unknown state.
	if err := sendTestNotification(req.Type, normalizedConfig, ""); err != nil {
		notificationsLog.Warn().Err(err).Str("type", string(req.Type)).Msg("test notification failed while creating notification")
		respondTestNotificationError(c, err)
		return
	}

	notification := models.Notification{
		Name:            crypto.EncryptedString(normalizedName),
		Enabled:         enabled,
		AllApplications: allApplications,
		AllAgents:       allAgents,
		AllRepositories: allRepositories,
		Events:          events,
		Status:          models.NotificationStatusSuccess,
		Type:            req.Type,
		Config:          crypto.EncryptedString(normalizedConfig),
	}

	err = db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := gorm.G[models.Notification](tx).Select("*").Create(ctx, &notification); err != nil {
			return err
		}

		return targets.replace(tx, &notification)
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

	c.JSON(http.StatusCreated, toNotificationResponse(perms, &createdNotification))
	sse.PublishUpdate(sse.TopicNotifications)
}

func UpdateNotificationHandler(c *gin.Context) {
	id := c.Param("id")

	var req updateNotificationRequest
	if err := c.ShouldBindJSON(&req); err != nil ||
		req.Enabled == nil || req.AllApplications == nil || req.AllAgents == nil || req.AllRepositories == nil || req.Events == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: enabled, allApplications, allAgents, allRepositories and events are required"})
		return
	}

	events, err := normalizeNotificationEvents(req.Events)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	notification, err := getNotificationById(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "notification not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	perms, ok := currentPermissions(c)
	if !ok {
		return
	}
	applicationIds, agentIds, repositoryIds, ok := authorizeNotificationTargets(c, perms, &notification, notificationScope{
		allApplications: *req.AllApplications, allAgents: *req.AllAgents, allRepositories: *req.AllRepositories,
		applicationIds: req.ApplicationIds, agentIds: req.AgentIds, repositoryIds: req.RepositoryIds,
	})
	if !ok {
		return
	}

	targets, ok := loadNotificationTargets(c,
		scopedNotificationResourceIds(*req.AllApplications, applicationIds),
		scopedNotificationResourceIds(*req.AllAgents, agentIds),
		scopedNotificationResourceIds(*req.AllRepositories, repositoryIds),
	)
	if !ok {
		return
	}

	err = db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		updates := models.Notification{
			Enabled:         *req.Enabled,
			AllApplications: *req.AllApplications,
			AllAgents:       *req.AllAgents,
			AllRepositories: *req.AllRepositories,
			Events:          events,
		}
		rowsAffected, err := gorm.G[models.Notification](tx).
			Where("id = ?", id).
			Select("enabled", "all_applications", "all_agents", "all_repositories", "events").
			Updates(ctx, updates)
		if err != nil {
			return err
		}
		if rowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}

		return targets.replace(tx, &notification)
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
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "notification not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, toNotificationResponse(perms, &updatedNotification))
	sse.PublishUpdate(sse.TopicNotifications)
}

// SetNotificationEnabledHandler only toggles the enabled flag, so it cannot overwrite a concurrent
// edit of the subscription.
func SetNotificationEnabledHandler(c *gin.Context) {
	id := c.Param("id")

	var req setNotificationEnabledRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: enabled is required"})
		return
	}
	perms, ok := currentPermissions(c)
	if !ok {
		return
	}

	ctx := c.Request.Context()
	rowsAffected, err := gorm.G[models.Notification](db.DB).
		Where("id = ?", id).
		Update(ctx, "enabled", *req.Enabled)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "notification not found"})
		return
	}

	notification, err := getNotificationById(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "notification not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, toNotificationResponse(perms, &notification))
	sse.PublishUpdate(sse.TopicNotifications)
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

	pruneGrants(c.Request.Context())

	c.JSON(http.StatusOK, gin.H{"message": "notification deleted"})
	sse.PublishUpdate(sse.TopicNotifications)
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
		notificationsLog.Warn().Err(sendErr).Str("notificationId", notification.Id).Msg("test notification failed")
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

	sse.PublishUpdate(sse.TopicNotifications)

	if sendErr != nil {
		respondTestNotificationError(c, sendErr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "test notification sent"})
}

// respondTestNotificationError exposes the failure reason so users can fix
// their config. The errors are already stripped of credentials.
func respondTestNotificationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, hubnotifications.ErrInvalidNotificationConfig):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, hubnotifications.ErrNotificationDispatch):
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
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
		Preload("Applications", selectNotificationResourceId).
		Preload("Agents", selectNotificationResourceId).
		Preload("Repositories", selectNotificationResourceId).
		Where("id = ?", id).
		First(ctx)
}

func selectNotificationResourceId(db gorm.PreloadBuilder) error {
	db.Select("id")
	return nil
}

// toNotificationResponse leaves out the targets the user cannot read.
func toNotificationResponse(perms *rbac.Permissions, notification *models.Notification) notificationResponse {
	response := notificationResponse{
		Id:              notification.Id,
		Name:            notification.Name.String(),
		Enabled:         notification.Enabled,
		AllApplications: notification.AllApplications,
		AllAgents:       notification.AllAgents,
		AllRepositories: notification.AllRepositories,
		Events:          notification.Events,
		Status:          string(notification.Status),
		Type:            string(notification.Type),
		ApplicationIds:  readableIds(perms, models.ResourceApplication, sortedIds(notification.Applications, applicationIdOf)),
		AgentIds:        readableIds(perms, models.ResourceAgent, sortedIds(notification.Agents, agentIdOf)),
		RepositoryIds:   readableIds(perms, models.ResourceRepository, sortedIds(notification.Repositories, repositoryIdOf)),
		CreatedAt:       notification.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       notification.UpdatedAt.Format(time.RFC3339),
	}

	return response
}

func isValidNotificationType(notificationType models.NotificationType) bool {
	_, err := provider.Get(notificationType)
	return err == nil
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
	targets, err := provider.BuildShoutrrrUrls(notificationType, rawConfig)
	if err != nil {
		return err
	}

	if _, err := shoutrrr.CreateSenderWithOptions(types.SenderOptions{}, targets...); err != nil {
		return err
	}

	return nil
}

func normalizeNotificationConfig(raw json.RawMessage) (string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return "", errors.New("invalid config: must be a non-empty string or JSON object")
	}

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		if strings.HasPrefix(trimmed, "\"") {
			return "", errors.New("invalid config: expected a valid JSON string")
		}

		return "", errors.New("invalid config: expected valid JSON")
	}

	if value == nil {
		return "", errors.New("invalid config: must be a non-empty string or JSON object")
	}

	if stringValue, ok := value.(string); ok {
		stringValue = strings.TrimSpace(stringValue)
		if stringValue == "" {
			return "", errors.New("invalid config: must not be empty")
		}

		return stringValue, nil
	}

	return trimmed, nil
}

// normalizeNotificationEvents validates events, drops duplicates and returns
// them in catalog order.
func normalizeNotificationEvents(events []models.NotificationEvent) ([]models.NotificationEvent, error) {
	if len(events) == 0 {
		return nil, errors.New("at least one event is required")
	}
	for _, event := range events {
		if !event.IsValid() {
			return nil, errors.New("invalid event: " + string(event))
		}
	}

	normalized := make([]models.NotificationEvent, 0, len(events))
	for _, event := range models.NotificationEvents {
		if slices.Contains(events, event) {
			normalized = append(normalized, event)
		}
	}

	return normalized, nil
}

// notificationTargets holds the resources a notification is explicitly
// assigned to.
type notificationTargets struct {
	applications []models.Application
	agents       []models.Agent
	repositories []models.Repository
}

func (t *notificationTargets) replace(tx *gorm.DB, notification *models.Notification) error {
	if err := tx.Model(notification).Association("Applications").Replace(t.applications); err != nil {
		return err
	}
	if err := tx.Model(notification).Association("Agents").Replace(t.agents); err != nil {
		return err
	}
	return tx.Model(notification).Association("Repositories").Replace(t.repositories)
}

// loadNotificationTargets loads the assigned resources and writes the error
// response if one of them does not exist.
func loadNotificationTargets(c *gin.Context, applicationIds, agentIds, repositoryIds []string) (notificationTargets, bool) {
	ctx := c.Request.Context()
	var targets notificationTargets
	var missing string
	var err error

	if targets.applications, missing, err = loadNotificationResources(ctx, applicationIds, applicationIdOf); err != nil || missing != "" {
		respondNotificationResourceError(c, "application", missing, err)
		return targets, false
	}
	if targets.agents, missing, err = loadNotificationResources(ctx, agentIds, agentIdOf); err != nil || missing != "" {
		respondNotificationResourceError(c, "agent", missing, err)
		return targets, false
	}
	if targets.repositories, missing, err = loadNotificationResources(ctx, repositoryIds, repositoryIdOf); err != nil || missing != "" {
		respondNotificationResourceError(c, "repository", missing, err)
		return targets, false
	}

	return targets, true
}

func respondNotificationResourceError(c *gin.Context, resource, missingId string, err error) {
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": resource + " not found: " + missingId})
}

// loadNotificationResources loads the resources with the given IDs. It returns
// the first ID that does not exist, if any.
func loadNotificationResources[T any](ctx context.Context, ids []string, idOf func(*T) string) ([]T, string, error) {
	normalizedIds := normalizeNotificationResourceIds(ids)
	if len(normalizedIds) == 0 {
		return []T{}, "", nil
	}

	resources, err := gorm.G[T](db.DB).Where("id IN ?", normalizedIds).Find(ctx)
	if err != nil {
		return nil, "", err
	}
	if len(resources) != len(normalizedIds) {
		foundById := make(map[string]struct{}, len(resources))
		for i := range resources {
			foundById[idOf(&resources[i])] = struct{}{}
		}
		for _, id := range normalizedIds {
			if _, ok := foundById[id]; !ok {
				return nil, id, nil
			}
		}
	}

	return resources, "", nil
}

// scopedNotificationResourceIds drops explicit assignments when the notification
// already covers every resource of that type.
func scopedNotificationResourceIds(all bool, ids []string) []string {
	if all {
		return nil
	}
	return ids
}

func normalizeNotificationResourceIds(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}

	normalized := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))

	for i := range ids {
		id := strings.TrimSpace(ids[i])
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

type notificationScope struct {
	allApplications, allAgents, allRepositories bool
	applicationIds, agentIds, repositoryIds     []string
}

// authorizeNotificationTargets makes sure users only subscribe notifications to
// resources they can read, since the notification tells about them. Targets of an
// existing notification the user cannot see are kept, as they were not sent back.
func authorizeNotificationTargets(c *gin.Context, perms *rbac.Permissions, existing *models.Notification, scope notificationScope) ([]string, []string, []string, bool) {
	var wasAll [3]bool
	var existingIds [3][]string
	if existing != nil {
		wasAll = [3]bool{existing.AllApplications, existing.AllAgents, existing.AllRepositories}
		existingIds = [3][]string{
			sortedIds(existing.Applications, applicationIdOf),
			sortedIds(existing.Agents, agentIdOf),
			sortedIds(existing.Repositories, repositoryIdOf),
		}
	}

	resources := [3]models.PermissionResource{models.ResourceApplication, models.ResourceAgent, models.ResourceRepository}
	all := [3]bool{scope.allApplications, scope.allAgents, scope.allRepositories}
	requested := [3][]string{scope.applicationIds, scope.agentIds, scope.repositoryIds}
	var result [3][]string

	for i, resource := range resources {
		if all[i] && !wasAll[i] && !perms.CanAll(resource, models.ActionRead) {
			respondForbidden(c, "insufficient permissions to subscribe to all "+string(resource)+"s")
			return nil, nil, nil, false
		}

		ids := make([]string, 0, len(requested[i]))
		for _, id := range normalizeNotificationResourceIds(requested[i]) {
			if !perms.Can(resource, models.ActionRead, id) && !slices.Contains(existingIds[i], id) {
				respondForbidden(c, "insufficient permissions for "+string(resource)+": "+id)
				return nil, nil, nil, false
			}
			ids = append(ids, id)
		}
		for _, id := range existingIds[i] {
			if !perms.Can(resource, models.ActionRead, id) && !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
		result[i] = ids
	}

	return result[0], result[1], result[2], true
}

func readableIds(perms *rbac.Permissions, resource models.PermissionResource, ids []string) []string {
	readable := make([]string, 0, len(ids))
	for _, id := range ids {
		if perms.Can(resource, models.ActionRead, id) {
			readable = append(readable, id)
		}
	}
	return readable
}

func applicationIdOf(a *models.Application) string { return a.Id }
func agentIdOf(a *models.Agent) string             { return a.Id }
func repositoryIdOf(r *models.Repository) string   { return r.Id }

func sortedIds[T any](resources []T, idOf func(*T) string) []string {
	ids := make([]string, 0, len(resources))
	for i := range resources {
		ids = append(ids, idOf(&resources[i]))
	}
	sort.Strings(ids)
	return ids
}
