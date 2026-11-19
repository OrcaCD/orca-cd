package routes

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/OrcaCD/orca-cd/internal/shared/logger"
	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const maxApplicationEventLimit = 100

var applicationEventsLog = logger.New("application-events", false)

const applicationEventSearchCondition = "(type LIKE @search OR source LIKE @search OR status LIKE @search" +
	" OR actor_name LIKE @search OR commit_hash LIKE @search OR commit_message LIKE @search OR error_message LIKE @search)"

type applicationEventResponse struct {
	Id            string  `json:"id" required:"true"`
	CreatedAt     string  `json:"createdAt" required:"true" format:"date-time"`
	CompletedAt   *string `json:"completedAt" required:"true" nullable:"true" format:"date-time"`
	Type          string  `json:"type" required:"true"`
	Source        string  `json:"source" required:"true" doc:"What triggered the event"`
	Status        string  `json:"status" required:"true"`
	ActorName     *string `json:"actorName" required:"true" nullable:"true" doc:"The user who triggered the event"`
	CommitHash    *string `json:"commitHash" required:"true" nullable:"true"`
	CommitMessage *string `json:"commitMessage" required:"true" nullable:"true"`
	ErrorMessage  *string `json:"errorMessage" required:"true" nullable:"true"`
	// DeploymentId is set while the deployment the event deployed or rolled
	// back to is still recorded.
	DeploymentId     *string `json:"deploymentId" required:"true" nullable:"true" doc:"The deployment the event deployed or rolled back to, while it is still recorded"`
	DeploymentStatus *string `json:"deploymentStatus" required:"true" nullable:"true"`
	// WaitingForAgent is set while the health of the deployment is checked
	// but its agent is not connected to report it.
	WaitingForAgent bool `json:"waitingForAgent,omitempty" doc:"Set while the health of the deployment is checked but its agent is not connected"`
}

type applicationEventsOutput struct {
	Body struct {
		Items   []applicationEventResponse `json:"items" required:"true" doc:"The events, newest first"`
		HasMore bool                       `json:"hasMore" required:"true" doc:"Whether there are older events"`
	}
}

var ListApplicationEventsHandler = restapi.Handler(huma.Operation{
	OperationID: "list-application-events",
	Method:      http.MethodGet,
	Path:        "/applications/{id}/events",
	Summary:     "List the events of an application",
	Description: "Lists the deployments, syncs and rollbacks of the application, newest first. " +
		requires(models.ResourceApplication, models.ActionRead),
	Tags:     []string{tagApplications},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusBadRequest, http.StatusNotFound, http.StatusForbidden},
}, func(ctx context.Context, input *struct {
	ID     string `path:"id" doc:"The ID of the application"`
	Limit  int    `query:"limit" default:"20" doc:"Between 1 and 100"`
	Offset int    `query:"offset" doc:"At least 0"`
	Search string `query:"search" doc:"Only returns events whose type, source, status, actor, commit or error contains it"`
}) (*applicationEventsOutput, error) {
	applicationID := input.ID

	if input.Limit < 1 || input.Limit > maxApplicationEventLimit || input.Offset < 0 {
		return nil, huma.Error400BadRequest("invalid pagination")
	}
	limit, offset := input.Limit, input.Offset

	if _, err := gorm.G[models.Application](db.DB).
		Select("id").
		Where("id = ?", applicationID).
		First(ctx); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("application not found")
		}
		applicationEventsLog.Error().Err(err).Str("applicationId", applicationID).
			Msg("failed to load application for event history")
		return nil, internalError()
	}

	query := gorm.G[models.ApplicationEvent](db.DB).Where("application_id = ?", applicationID)
	if search := strings.TrimSpace(input.Search); search != "" {
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
		return nil, internalError()
	}

	hasMore := len(events) > limit
	if hasMore {
		events = events[:limit]
	}
	items, err := toApplicationEventResponses(ctx, events)
	if err != nil {
		applicationEventsLog.Error().Err(err).Str("applicationId", applicationID).
			Msg("failed to load deployments of application events")
		return nil, internalError()
	}

	out := &applicationEventsOutput{}
	out.Body.Items = items
	out.Body.HasMore = hasMore
	return out, nil
})

// toApplicationEventResponses converts events and adds the deployments they
// reference that are still recorded.
func toApplicationEventResponses(ctx context.Context, events []models.ApplicationEvent) ([]applicationEventResponse, error) {
	states, err := deploymentStates(ctx, events)
	if err != nil {
		return nil, err
	}
	items := make([]applicationEventResponse, 0, len(events))
	for i := range events {
		item := toApplicationEventResponse(&events[i])
		if id := events[i].DeploymentId; id != nil {
			if state, ok := states[*id]; ok {
				status := string(state.status)
				item.DeploymentId = id
				item.DeploymentStatus = &status
				item.WaitingForAgent = state.waitingForAgent
			}
		}
		items = append(items, item)
	}
	return items, nil
}

type deploymentState struct {
	status          models.ApplicationDeploymentStatus
	waitingForAgent bool
}

// deploymentStates returns the state of every deployment the events
// reference that is still recorded.
func deploymentStates(ctx context.Context, events []models.ApplicationEvent) (map[string]deploymentState, error) {
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
		Select("id", "application_id", "status").
		Where("id IN ?", ids).
		Find(ctx)
	if err != nil {
		return nil, err
	}
	offline, err := offlineApplications(ctx, deployments)
	if err != nil {
		return nil, err
	}
	states := make(map[string]deploymentState, len(deployments))
	for i := range deployments {
		states[deployments[i].Id] = deploymentState{
			status:          deployments[i].Status,
			waitingForAgent: deployments[i].Status == models.DeploymentVerifying && offline[deployments[i].ApplicationId],
		}
	}
	return states, nil
}

// offlineApplications returns the applications of the verifying deployments
// whose agent is not connected and healthy, so it can't report their health.
func offlineApplications(ctx context.Context, deployments []models.ApplicationDeployment) (map[string]bool, error) {
	var appIDs []string
	for i := range deployments {
		if deployments[i].Status == models.DeploymentVerifying {
			appIDs = append(appIDs, deployments[i].ApplicationId)
		}
	}
	if len(appIDs) == 0 {
		return nil, nil
	}
	onlineAgents := gorm.G[models.Agent](db.DB).Select("id").Where("status = ?", models.AgentStatusOnline)
	apps, err := gorm.G[models.Application](db.DB).
		Select("id").
		Where("id IN ? AND agent_id NOT IN (?)", appIDs, onlineAgents).
		Find(ctx)
	if err != nil {
		return nil, err
	}
	offline := make(map[string]bool, len(apps))
	for i := range apps {
		offline[apps[i].Id] = true
	}
	return offline, nil
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
