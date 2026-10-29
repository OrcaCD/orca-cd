package routes

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/shared/logger"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	defaultApplicationEventLimit = 20
	maxApplicationEventLimit     = 100
)

var applicationEventsLog = logger.New("application-events", false)

const applicationEventSearchCondition = "(type LIKE @search OR source LIKE @search OR status LIKE @search" +
	" OR actor_name LIKE @search OR commit_hash LIKE @search OR commit_message LIKE @search OR error_message LIKE @search)"

type applicationEventResponse struct {
	Id            string  `json:"id"`
	CreatedAt     string  `json:"createdAt"`
	CompletedAt   *string `json:"completedAt"`
	Type          string  `json:"type"`
	Source        string  `json:"source"`
	Status        string  `json:"status"`
	ActorName     *string `json:"actorName"`
	CommitHash    *string `json:"commitHash"`
	CommitMessage *string `json:"commitMessage"`
	ErrorMessage  *string `json:"errorMessage"`
	// DeploymentId is set while the deployment the event deployed or rolled
	// back to is still recorded.
	DeploymentId     *string `json:"deploymentId"`
	DeploymentStatus *string `json:"deploymentStatus"`
}

func ListApplicationEventsHandler(c *gin.Context) {
	ctx := c.Request.Context()
	applicationID := c.Param("id")

	limit, offset, ok := applicationEventPagination(c)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pagination"})
		return
	}

	if _, err := gorm.G[models.Application](db.DB).
		Select("id").
		Where("id = ?", applicationID).
		First(ctx); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "application not found"})
			return
		}
		applicationEventsLog.Error().Err(err).Str("applicationId", applicationID).
			Msg("failed to load application for event history")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	query := gorm.G[models.ApplicationEvent](db.DB).Where("application_id = ?", applicationID)
	if search := strings.TrimSpace(c.Query("search")); search != "" {
		query = query.Where(applicationEventSearchCondition, sql.Named("search", "%"+search+"%"))
	}

	events, err := query.
		Order("created_at DESC, id DESC").
		Offset(offset).
		Limit(limit + 1).
		Find(ctx)
	if err != nil {
		applicationEventsLog.Error().Err(err).Str("applicationId", applicationID).
			Msg("failed to list application events")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	hasMore := len(events) > limit
	if hasMore {
		events = events[:limit]
	}
	items, err := toApplicationEventResponses(ctx, events)
	if err != nil {
		applicationEventsLog.Error().Err(err).Str("applicationId", applicationID).
			Msg("failed to load deployments of application events")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"items": items, "hasMore": hasMore})
}

// toApplicationEventResponses converts events and adds the deployments they
// reference that are still recorded.
func toApplicationEventResponses(ctx context.Context, events []models.ApplicationEvent) ([]applicationEventResponse, error) {
	statuses, err := deploymentStatuses(ctx, events)
	if err != nil {
		return nil, err
	}
	items := make([]applicationEventResponse, 0, len(events))
	for i := range events {
		item := toApplicationEventResponse(&events[i])
		if id := events[i].DeploymentId; id != nil {
			if status, ok := statuses[*id]; ok {
				item.DeploymentId = id
				item.DeploymentStatus = &status
			}
		}
		items = append(items, item)
	}
	return items, nil
}

// deploymentStatuses returns the status of every deployment the events
// reference that is still recorded.
func deploymentStatuses(ctx context.Context, events []models.ApplicationEvent) (map[string]string, error) {
	ids := make([]string, 0, len(events))
	for i := range events {
		if events[i].DeploymentId != nil {
			ids = append(ids, *events[i].DeploymentId)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	deployments, err := gorm.G[models.ApplicationDeployment](db.DB).
		Select("id", "status").
		Where("id IN ?", ids).
		Find(ctx)
	if err != nil {
		return nil, err
	}
	statuses := make(map[string]string, len(deployments))
	for i := range deployments {
		statuses[deployments[i].Id] = string(deployments[i].Status)
	}
	return statuses, nil
}

func applicationEventPagination(c *gin.Context) (int, int, bool) {
	limit := defaultApplicationEventLimit
	if raw := c.Query("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > maxApplicationEventLimit {
			return 0, 0, false
		}
		limit = value
	}

	offset := 0
	if raw := c.Query("offset"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			return 0, 0, false
		}
		offset = value
	}
	return limit, offset, true
}

func toApplicationEventResponse(event *models.ApplicationEvent) applicationEventResponse {
	var completedAt *string
	if event.CompletedAt != nil {
		formatted := event.CompletedAt.UTC().Format(time.RFC3339Nano)
		completedAt = &formatted
	}
	return applicationEventResponse{
		Id:            event.Id,
		CreatedAt:     event.CreatedAt.UTC().Format(time.RFC3339Nano),
		CompletedAt:   completedAt,
		Type:          string(event.Type),
		Source:        string(event.Source),
		Status:        string(event.Status),
		ActorName:     event.ActorName,
		CommitHash:    event.CommitHash,
		CommitMessage: event.CommitMessage,
		ErrorMessage:  event.ErrorMessage,
	}
}

func eventActor(c *gin.Context) (*string, *string) {
	user, ok := auth.GetUser(c)
	if !ok {
		return nil, nil
	}
	userID := user.Id
	userName := user.Name
	return &userID, &userName
}
