package routes

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	applicationActivityDays              = 7
	applicationActivityRecentDeployments = 8
	// UTC offsets range from -12:00 to +14:00.
	maxUTCOffsetMinutes = 14 * 60
)

type applicationActivityDay struct {
	Date      string `json:"date"`
	Succeeded int    `json:"succeeded"`
	Failed    int    `json:"failed"`
}

type applicationActivityDeployment struct {
	applicationEventResponse
	ApplicationId   string `json:"applicationId"`
	ApplicationName string `json:"applicationName"`
	ApplicationIcon string `json:"applicationIcon"`
}

type applicationActivityResponse struct {
	Days              []applicationActivityDay        `json:"days"`
	RecentDeployments []applicationActivityDeployment `json:"recentDeployments"`
}

// ApplicationActivityHandler summarizes deployments across all applications.
// Days are bucketed in the caller's timezone, passed as utcOffset in minutes
// east of UTC, so the chart lines up with the user's calendar days.
func ApplicationActivityHandler(c *gin.Context) {
	ctx := c.Request.Context()

	offset, ok := parseUTCOffset(c.Query("utcOffset"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid utcOffset"})
		return
	}

	zone := time.FixedZone("", offset*60)
	now := time.Now().In(zone)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone)
	since := today.AddDate(0, 0, -(applicationActivityDays - 1))

	completed := []models.ApplicationEventStatus{models.ApplicationEventSucceeded, models.ApplicationEventFailed}
	// Timestamps are stored as strings in the hub's local zone, so compare against
	// a local time and leave the exact day filtering to the bucketing below.
	events, err := gorm.G[models.ApplicationEvent](db.DB).
		Select("created_at", "status").
		Where("status IN ? AND created_at >= ?", completed, since.Add(-24*time.Hour).Local()).
		Find(ctx)
	if err != nil {
		applicationEventsLog.Error().Err(err).Msg("failed to load application activity")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	days := make([]applicationActivityDay, applicationActivityDays)
	for i := range days {
		days[i].Date = since.AddDate(0, 0, i).Format(time.DateOnly)
	}
	for i := range events {
		created := events[i].CreatedAt.In(zone)
		day := time.Date(created.Year(), created.Month(), created.Day(), 0, 0, 0, 0, zone)
		index := int(day.Sub(since).Hours() / 24)
		if index < 0 || index >= len(days) {
			continue
		}
		if events[i].Status == models.ApplicationEventSucceeded {
			days[index].Succeeded++
		} else {
			days[index].Failed++
		}
	}

	recentDeployments, err := recentDeployments(ctx)
	if err != nil {
		applicationEventsLog.Error().Err(err).Msg("failed to load recent deployments")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, applicationActivityResponse{Days: days, RecentDeployments: recentDeployments})
}

// recentDeployments returns the newest events across all applications. Events
// without changes are left out, since polling would otherwise flood the feed.
func recentDeployments(ctx context.Context) ([]applicationActivityDeployment, error) {
	events, err := gorm.G[models.ApplicationEvent](db.DB).
		Preload("Application", nil).
		Where("status IN ?", []models.ApplicationEventStatus{
			models.ApplicationEventRunning,
			models.ApplicationEventSucceeded,
			models.ApplicationEventFailed,
		}).
		Order("created_at DESC, id DESC").
		Limit(applicationActivityRecentDeployments).
		Find(ctx)
	if err != nil {
		return nil, err
	}

	items, err := toApplicationEventResponses(ctx, events)
	if err != nil {
		return nil, err
	}
	deployments := make([]applicationActivityDeployment, 0, len(events))
	for i := range events {
		deployments = append(deployments, applicationActivityDeployment{
			applicationEventResponse: items[i],
			ApplicationId:            events[i].ApplicationId,
			ApplicationName:          events[i].Application.Name.String(),
			ApplicationIcon:          events[i].Application.Icon,
		})
	}
	return deployments, nil
}

func parseUTCOffset(raw string) (int, bool) {
	if raw == "" {
		return 0, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < -maxUTCOffsetMinutes || value > maxUTCOffsetMinutes {
		return 0, false
	}
	return value, true
}
