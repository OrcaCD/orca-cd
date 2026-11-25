package routes

import (
	"context"
	"net/http"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/danielgtaylor/huma/v2"
	"gorm.io/gorm"
)

const (
	applicationActivityDays              = 7
	applicationActivityRecentDeployments = 8
	// UTC offsets range from -12:00 to +14:00.
	maxUTCOffsetMinutes = 14 * 60
)

type applicationActivityDay struct {
	Date      string `json:"date" required:"true" format:"date"`
	Succeeded int    `json:"succeeded" required:"true" doc:"Number of successful deployments on the day"`
	Failed    int    `json:"failed" required:"true" doc:"Number of failed deployments on the day"`
}

type applicationActivityDeployment struct {
	applicationEventResponse
	ApplicationId   string `json:"applicationId" required:"true"`
	ApplicationName string `json:"applicationName" required:"true"`
	ApplicationIcon string `json:"applicationIcon" required:"true"`
}

type applicationActivityResponse struct {
	Days              []applicationActivityDay        `json:"days" required:"true" doc:"The last 7 days, oldest first"`
	RecentDeployments []applicationActivityDeployment `json:"recentDeployments" required:"true" doc:"The newest events with changes, newest first"`
}

// ApplicationActivityHandler summarizes deployments across all applications.
// Days are bucketed in the caller's timezone, passed as utcOffset in minutes
// east of UTC, so the chart lines up with the user's calendar days.
var ApplicationActivityHandler = restapi.Handler(huma.Operation{
	OperationID: "get-application-activity",
	Method:      http.MethodGet,
	Path:        "/activity",
	Summary:     "Get the deployment activity",
	Description: "Counts the deployments of the last 7 days and lists the newest events across the applications the user may view.",
	Tags:        []string{tagApplications},
	Security:    restapi.Authenticated,
	Errors:      []int{http.StatusBadRequest},
}, func(ctx context.Context, input *struct {
	UTCOffset int `query:"utcOffset" doc:"The timezone of the days in minutes east of UTC, between -840 and 840"`
}) (*struct{ Body applicationActivityResponse }, error) {
	perms, err := permissionsFor(restapi.GinContext(ctx))
	if err != nil {
		return nil, err
	}
	visible := perms.Scope(models.ResourceApplication, models.ActionRead, "application_id")

	offset := input.UTCOffset
	if offset < -maxUTCOffsetMinutes || offset > maxUTCOffsetMinutes {
		return nil, huma.Error400BadRequest("invalid utcOffset")
	}

	zone := time.FixedZone("", offset*60)
	now := time.Now().In(zone)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone)
	since := today.AddDate(0, 0, -(applicationActivityDays - 1))

	completed := []models.ApplicationEventStatus{models.ApplicationEventSucceeded, models.ApplicationEventFailed}
	// Timestamps are stored as strings in the hub's local zone, so compare against
	// a local time and leave the exact day filtering to the bucketing below.
	events, err := gorm.G[models.ApplicationEvent](db.DB).
		Scopes(visible).
		Select("created_at", "status").
		Where("status IN ? AND created_at >= ?", completed, since.Add(-24*time.Hour).Local()).
		Find(ctx)
	if err != nil {
		applicationEventsLog.Error().Err(err).Msg("failed to load application activity")
		return nil, internalError()
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

	recentDeployments, err := recentDeployments(ctx, visible)
	if err != nil {
		applicationEventsLog.Error().Err(err).Msg("failed to load recent deployments")
		return nil, internalError()
	}

	return &struct{ Body applicationActivityResponse }{
		Body: applicationActivityResponse{Days: days, RecentDeployments: recentDeployments},
	}, nil
})

// recentDeployments returns the newest events across all applications. Events
// without changes are left out, since polling would otherwise flood the feed.
func recentDeployments(ctx context.Context, visible func(*gorm.Statement)) ([]applicationActivityDeployment, error) {
	events, err := gorm.G[models.ApplicationEvent](db.DB).
		Scopes(visible).
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
