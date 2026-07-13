package routes

import (
	"errors"
	"net/http"
	"strconv"
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

	events, err := gorm.G[models.ApplicationEvent](db.DB).
		Where("application_id = ?", applicationID).
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
	items := make([]applicationEventResponse, 0, len(events))
	for i := range events {
		items = append(items, toApplicationEventResponse(&events[i]))
	}

	c.JSON(http.StatusOK, gin.H{"items": items, "hasMore": hasMore})
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
	claims, ok := auth.GetClaims(c)
	if !ok {
		return nil, nil
	}
	userID := claims.Subject
	userName := claims.Name
	return &userID, &userName
}
