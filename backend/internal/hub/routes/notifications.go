package routes

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/OrcaCD/orca-cd/internal/shared/logger"
	"github.com/danielgtaylor/huma/v2"
	"github.com/nicholas-fedor/shoutrrr"
	"github.com/nicholas-fedor/shoutrrr/pkg/types"
	"gorm.io/gorm"
)

type createNotificationRequest struct {
	Name            string                     `json:"name" required:"true" minLength:"1" maxLength:"128"`
	Enabled         *bool                      `json:"enabled" doc:"Defaults to true"`
	AllApplications *bool                      `json:"allApplications" doc:"Tell about every application, including future ones"`
	AllAgents       *bool                      `json:"allAgents" doc:"Tell about every agent, including future ones"`
	AllRepositories *bool                      `json:"allRepositories" doc:"Tell about every repository, including future ones"`
	Events          []models.NotificationEvent `json:"events,omitempty" doc:"The events to tell about. Leave it out for all events."`
	Type            models.NotificationType    `json:"type" required:"true" minLength:"1" doc:"The provider, e.g. discord or webhook"`
	Config          json.RawMessage            `json:"config" required:"true" doc:"The settings of the provider, as a JSON object or a string. It holds secrets and is never returned."`
	ApplicationIds  []string                   `json:"applicationIds,omitempty" doc:"Ignored when allApplications is set"`
	AgentIds        []string                   `json:"agentIds,omitempty" doc:"Ignored when allAgents is set"`
	RepositoryIds   []string                   `json:"repositoryIds,omitempty" doc:"Ignored when allRepositories is set"`
}

type setNotificationEnabledRequest struct {
	Enabled bool `json:"enabled" required:"true"`
}

type updateNotificationRequest struct {
	Enabled         bool                       `json:"enabled" required:"true"`
	AllApplications bool                       `json:"allApplications" required:"true"`
	AllAgents       bool                       `json:"allAgents" required:"true"`
	AllRepositories bool                       `json:"allRepositories" required:"true"`
	Events          []models.NotificationEvent `json:"events" required:"true" nullable:"false"`
	ApplicationIds  []string                   `json:"applicationIds,omitempty" doc:"Ignored when allApplications is set"`
	AgentIds        []string                   `json:"agentIds,omitempty" doc:"Ignored when allAgents is set"`
	RepositoryIds   []string                   `json:"repositoryIds,omitempty" doc:"Ignored when allRepositories is set"`
}

type testNotificationRequest struct {
	Message string `json:"message,omitempty" doc:"The text to send. Leave it out for the default test message."`
}

var sendTestNotification = hubnotifications.SendTestNotification

var notificationsLog = logger.New("notifications", false)

type notificationResponse struct {
	Id              string                     `json:"id" required:"true"`
	Name            string                     `json:"name" required:"true"`
	Enabled         bool                       `json:"enabled" required:"true"`
	AllApplications bool                       `json:"allApplications" required:"true"`
	AllAgents       bool                       `json:"allAgents" required:"true"`
	AllRepositories bool                       `json:"allRepositories" required:"true"`
	Events          []models.NotificationEvent `json:"events" required:"true"`
	Status          string                     `json:"status" required:"true" enum:"unknown,success,error" doc:"Whether the last notification was delivered"`
	Type            string                     `json:"type" required:"true"`
	ApplicationIds  []string                   `json:"applicationIds" required:"true" doc:"The assigned applications the user may view"`
	AgentIds        []string                   `json:"agentIds" required:"true" doc:"The assigned agents the user may view"`
	RepositoryIds   []string                   `json:"repositoryIds" required:"true" doc:"The assigned repositories the user may view"`
	CreatedAt       string                     `json:"createdAt" required:"true" format:"date-time"`
	UpdatedAt       string                     `json:"updatedAt" required:"true" format:"date-time"`
}

type notificationOutput struct {
	Body notificationResponse
}

var ListNotificationsHandler = restapi.Handler(huma.Operation{
	OperationID: "list-notifications",
	Method:      http.MethodGet,
	Path:        "/notifications",
	Summary:     "List notifications",
	Description: "Lists the notifications the user may view. Their configs hold secrets and are never returned.",
	Tags:        []string{tagNotifications},
	Security:    restapi.Authenticated,
}, func(ctx context.Context, _ *struct{}) (*struct{ Body []notificationResponse }, error) {
	perms, err := permissionsFor(restapi.GinContext(ctx))
	if err != nil {
		return nil, err
	}

	items, err := gorm.G[models.Notification](db.DB).
		Scopes(perms.Scope(models.ResourceNotification, models.ActionRead, "id")).
		Preload("Applications", selectNotificationResourceId).
		Preload("Agents", selectNotificationResourceId).
		Preload("Repositories", selectNotificationResourceId).
		Order("created_at ASC").
		Find(ctx)
	if err != nil {
		return nil, internalError()
	}

	response := make([]notificationResponse, 0, len(items))
	for i := range items {
		response = append(response, toNotificationResponse(perms, &items[i]))
	}

	return &struct{ Body []notificationResponse }{Body: response}, nil
})

var CreateNotificationHandler = restapi.Handler(huma.Operation{
	OperationID:   "create-notification",
	Method:        http.MethodPost,
	Path:          "/notifications",
	DefaultStatus: http.StatusCreated,
	Summary:       "Create a notification",
	Description: "Sends a test notification first and only saves the notification when it was delivered. " +
		"The user needs to be able to view every resource it tells about. " +
		requires(models.ResourceNotification, models.ActionCreate),
	Tags:     []string{tagNotifications},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusBadRequest, http.StatusForbidden, http.StatusBadGateway},
}, func(ctx context.Context, input *struct{ Body createNotificationRequest }) (*notificationOutput, error) {
	req := input.Body
	normalizedName, err := normalizeNotificationName(req.Name)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}

	if !isValidNotificationType(req.Type) {
		return nil, huma.Error400BadRequest("invalid type")
	}

	normalizedConfig, err := normalizeNotificationConfig(req.Config)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	if err := validateNotificationConfig(req.Type, normalizedConfig); err != nil {
		return nil, huma.Error400BadRequest("invalid config: " + err.Error())
	}

	// Omitting events subscribes to everything, matching the behavior before
	// events became selectable.
	events := slices.Clone(models.NotificationEvents)
	if req.Events != nil {
		events, err = normalizeNotificationEvents(req.Events)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	allApplications := req.AllApplications != nil && *req.AllApplications
	allAgents := req.AllAgents != nil && *req.AllAgents
	allRepositories := req.AllRepositories != nil && *req.AllRepositories

	perms, err := permissionsFor(restapi.GinContext(ctx))
	if err != nil {
		return nil, err
	}
	if err := authorizeNotificationTargets(perms, notificationScope{
		allApplications: allApplications, allAgents: allAgents, allRepositories: allRepositories,
		applicationIds: req.ApplicationIds, agentIds: req.AgentIds, repositoryIds: req.RepositoryIds,
	}); err != nil {
		return nil, err
	}

	targets, err := loadNotificationTargets(ctx,
		scopedNotificationResourceIds(allApplications, req.ApplicationIds),
		scopedNotificationResourceIds(allAgents, req.AgentIds),
		scopedNotificationResourceIds(allRepositories, req.RepositoryIds),
	)
	if err != nil {
		return nil, err
	}

	// Reject configs that can't deliver before saving them, so new
	// notifications never start in an unknown state.
	if err := sendTestNotification(req.Type, normalizedConfig, ""); err != nil {
		notificationsLog.Warn().Err(err).Str("type", string(req.Type)).Msg("test notification failed while creating notification")
		return nil, testNotificationError(err)
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
		return nil, internalError()
	}

	createdNotification, err := getNotificationById(ctx, notification.Id)
	if err != nil {
		return nil, internalError()
	}

	sse.PublishUpdate(sse.TopicNotifications.For(notification.Id))
	return &notificationOutput{Body: toNotificationResponse(perms, &createdNotification)}, nil
})

var UpdateNotificationHandler = restapi.Handler(huma.Operation{
	OperationID: "update-notification",
	Method:      http.MethodPut,
	Path:        "/notifications/{id}",
	Summary:     "Update a notification",
	Description: "Replaces the events and the resources the notification tells about. Its name, type and config can't be changed. " +
		"The user needs to be able to view every resource it told and will tell about. " +
		requires(models.ResourceNotification, models.ActionUpdate),
	Tags:     []string{tagNotifications},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound},
}, func(ctx context.Context, input *struct {
	ID   string `path:"id" doc:"The ID of the notification"`
	Body updateNotificationRequest
}) (*notificationOutput, error) {
	id, req := input.ID, input.Body

	events, err := normalizeNotificationEvents(req.Events)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}

	notification, err := getNotificationById(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("notification not found")
		}
		return nil, internalError()
	}

	perms, err := permissionsFor(restapi.GinContext(ctx))
	if err != nil {
		return nil, err
	}
	if err := canChangeNotification(perms, &notification); err != nil {
		return nil, err
	}
	if err := authorizeNotificationTargets(perms, notificationScope{
		allApplications: req.AllApplications, allAgents: req.AllAgents, allRepositories: req.AllRepositories,
		applicationIds: req.ApplicationIds, agentIds: req.AgentIds, repositoryIds: req.RepositoryIds,
	}); err != nil {
		return nil, err
	}

	targets, err := loadNotificationTargets(ctx,
		scopedNotificationResourceIds(req.AllApplications, req.ApplicationIds),
		scopedNotificationResourceIds(req.AllAgents, req.AgentIds),
		scopedNotificationResourceIds(req.AllRepositories, req.RepositoryIds),
	)
	if err != nil {
		return nil, err
	}

	err = db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		updates := models.Notification{
			Enabled:         req.Enabled,
			AllApplications: req.AllApplications,
			AllAgents:       req.AllAgents,
			AllRepositories: req.AllRepositories,
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
			return nil, huma.Error404NotFound("notification not found")
		}
		return nil, internalError()
	}

	updatedNotification, err := getNotificationById(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("notification not found")
		}
		return nil, internalError()
	}

	sse.PublishUpdate(sse.TopicNotifications.For(id))
	return &notificationOutput{Body: toNotificationResponse(perms, &updatedNotification)}, nil
})

// SetNotificationEnabledHandler only toggles the enabled flag, so it cannot overwrite a concurrent
// edit of the subscription.
var SetNotificationEnabledHandler = restapi.Handler(huma.Operation{
	OperationID: "set-notification-enabled",
	Method:      http.MethodPut,
	Path:        "/notifications/{id}/enabled",
	Summary:     "Enable or disable a notification",
	Description: "Only changes the enabled flag, so it can't overwrite a concurrent update. " +
		"The user needs to be able to view every resource the notification tells about. " +
		requires(models.ResourceNotification, models.ActionUpdate),
	Tags:     []string{tagNotifications},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound},
}, func(ctx context.Context, input *struct {
	ID   string `path:"id" doc:"The ID of the notification"`
	Body setNotificationEnabledRequest
}) (*notificationOutput, error) {
	id, req := input.ID, input.Body

	perms, err := permissionsFor(restapi.GinContext(ctx))
	if err != nil {
		return nil, err
	}

	notification, err := getNotificationById(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("notification not found")
		}
		return nil, internalError()
	}
	if err := canChangeNotification(perms, &notification); err != nil {
		return nil, err
	}

	rowsAffected, err := gorm.G[models.Notification](db.DB).
		Where("id = ?", id).
		Update(ctx, "enabled", req.Enabled)
	if err != nil {
		return nil, internalError()
	}
	if rowsAffected == 0 {
		return nil, huma.Error404NotFound("notification not found")
	}
	notification.Enabled = req.Enabled

	sse.PublishUpdate(sse.TopicNotifications.For(id))
	return &notificationOutput{Body: toNotificationResponse(perms, &notification)}, nil
})

var DeleteNotificationHandler = restapi.Handler(huma.Operation{
	OperationID: "delete-notification",
	Method:      http.MethodDelete,
	Path:        "/notifications/{id}",
	Summary:     "Delete a notification",
	Description: requires(models.ResourceNotification, models.ActionDelete),
	Tags:        []string{tagNotifications},
	Security:    restapi.Authenticated,
	Errors:      []int{http.StatusNotFound, http.StatusForbidden},
}, func(ctx context.Context, input *idInput) (*restapi.MessageOutput, error) {
	rowsAffected, err := gorm.G[models.Notification](db.DB).Where("id = ?", input.ID).Delete(ctx)
	if err != nil {
		return nil, internalError()
	}
	if rowsAffected == 0 {
		return nil, huma.Error404NotFound("notification not found")
	}

	sse.PublishUpdate(sse.TopicNotifications)
	// After the update, so clients still get it before losing access
	pruneGrants(ctx)

	return restapi.Message("notification deleted"), nil
})

var TestNotificationHandler = restapi.Handler(huma.Operation{
	OperationID: "test-notification",
	Method:      http.MethodPost,
	Path:        "/notifications/{id}/test",
	Summary:     "Send a test notification",
	Description: "Sends a test message and sets the status of the notification to whether it was delivered. " +
		"A delivery failure answers 502 with the reason. " +
		requires(models.ResourceNotification, models.ActionUpdate),
	Tags:     []string{tagNotifications},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusBadRequest, http.StatusNotFound, http.StatusBadGateway, http.StatusForbidden},
}, func(ctx context.Context, input *struct {
	ID   string `path:"id" doc:"The ID of the notification"`
	Body *testNotificationRequest
}) (*restapi.MessageOutput, error) {
	id := input.ID

	notification, err := gorm.G[models.Notification](db.DB).Where("id = ?", id).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("notification not found")
		}
		return nil, internalError()
	}

	var message string
	if input.Body != nil {
		message = input.Body.Message
	}

	sendErr := sendTestNotification(notification.Type, notification.Config.String(), message)
	status := models.NotificationStatusSuccess
	if sendErr != nil {
		notificationsLog.Warn().Err(sendErr).Str("notificationId", notification.Id).Msg("test notification failed")
		status = models.NotificationStatusError
	}

	if err := updateNotificationStatus(ctx, notification.Id, status); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("notification not found")
		}
		return nil, internalError()
	}

	sse.PublishUpdate(sse.TopicNotifications.For(id))

	if sendErr != nil {
		return nil, testNotificationError(sendErr)
	}

	return restapi.Message("test notification sent"), nil
})

// testNotificationError exposes the failure reason so users can fix their
// config. The errors are already stripped of credentials.
func testNotificationError(err error) error {
	switch {
	case errors.Is(err, hubnotifications.ErrInvalidNotificationConfig):
		return huma.Error400BadRequest(err.Error())
	case errors.Is(err, hubnotifications.ErrNotificationDispatch):
		return huma.NewError(http.StatusBadGateway, err.Error())
	default:
		return internalError()
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

// loadNotificationTargets loads the assigned resources and returns a 400 error
// if one of them does not exist.
func loadNotificationTargets(ctx context.Context, applicationIds, agentIds, repositoryIds []string) (notificationTargets, error) {
	var targets notificationTargets
	var missing string
	var err error

	if targets.applications, missing, err = loadNotificationResources(ctx, applicationIds, applicationIdOf); err != nil || missing != "" {
		return targets, notificationResourceError("application", missing, err)
	}
	if targets.agents, missing, err = loadNotificationResources(ctx, agentIds, agentIdOf); err != nil || missing != "" {
		return targets, notificationResourceError("agent", missing, err)
	}
	if targets.repositories, missing, err = loadNotificationResources(ctx, repositoryIds, repositoryIdOf); err != nil || missing != "" {
		return targets, notificationResourceError("repository", missing, err)
	}

	return targets, nil
}

func notificationResourceError(resource, missingId string, err error) error {
	if err != nil {
		return internalError()
	}
	return huma.Error400BadRequest(resource + " not found: " + missingId)
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

// canReadNotificationScope checks that the user can read every resource the
// notification tells about.
func canReadNotificationScope(perms *rbac.Permissions, scope notificationScope) (string, bool) {
	resources := [3]models.PermissionResource{models.ResourceApplication, models.ResourceAgent, models.ResourceRepository}
	all := [3]bool{scope.allApplications, scope.allAgents, scope.allRepositories}
	ids := [3][]string{scope.applicationIds, scope.agentIds, scope.repositoryIds}

	for i, resource := range resources {
		if all[i] {
			if !perms.CanAll(resource, models.ActionRead) {
				return "all " + string(resource) + "s", false
			}
			continue
		}
		for _, id := range normalizeNotificationResourceIds(ids[i]) {
			if !perms.Can(resource, models.ActionRead, id) {
				return string(resource) + ": " + id, false
			}
		}
	}
	return "", true
}

// authorizeNotificationTargets makes sure users only subscribe notifications to
// resources they can read, since the notification tells about them.
func authorizeNotificationTargets(perms *rbac.Permissions, scope notificationScope) error {
	if denied, ok := canReadNotificationScope(perms, scope); !ok {
		return huma.Error403Forbidden("insufficient permissions for " + denied)
	}
	return nil
}

// canChangeNotification makes sure users only change notifications about
// resources they can read. Otherwise they could enable it or add events and
// learn about resources they cannot see.
func canChangeNotification(perms *rbac.Permissions, notification *models.Notification) error {
	if _, ok := canReadNotificationScope(perms, notificationScope{
		allApplications: notification.AllApplications,
		allAgents:       notification.AllAgents,
		allRepositories: notification.AllRepositories,
		applicationIds:  sortedIds(notification.Applications, applicationIdOf),
		agentIds:        sortedIds(notification.Agents, agentIdOf),
		repositoryIds:   sortedIds(notification.Repositories, repositoryIdOf),
	}); !ok {
		return huma.Error403Forbidden("insufficient permissions: the notification is about resources you cannot access")
	}
	return nil
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
