package mcpserver

import (
	"context"
	"errors"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"
)

// recentEventLimit is how many events get_application returns, enough to see
// why the last deploys failed.
const recentEventLimit = 10

var errApplicationNotFound = errors.New("application not found")

var listApplicationsTool = &mcp.Tool{
	Name:        "list_applications",
	Description: "Lists the applications the token can read, with their sync and health status, the agent that runs them and the last sync error.",
	Annotations: &mcp.ToolAnnotations{Title: "List applications", ReadOnlyHint: true, OpenWorldHint: new(false)},
}

var getApplicationTool = &mcp.Tool{
	Name:        "get_application",
	Description: "Returns an application with its compose file, the warnings found in it, the last sync error and its recent events, which include the errors of failed deploys.",
	Annotations: &mcp.ToolAnnotations{Title: "Get application", ReadOnlyHint: true, OpenWorldHint: new(false)},
}

type applicationSummary struct {
	Id             string     `json:"id"`
	Name           string     `json:"name"`
	SyncStatus     string     `json:"syncStatus"`
	HealthStatus   string     `json:"healthStatus"`
	RepositoryId   string     `json:"repositoryId"`
	RepositoryName string     `json:"repositoryName"`
	Branch         string     `json:"branch"`
	Path           string     `json:"path" jsonschema:"path of the compose file in the repository"`
	Commit         string     `json:"commit"`
	AgentId        string     `json:"agentId"`
	AgentName      string     `json:"agentName"`
	LastSyncedAt   *time.Time `json:"lastSyncedAt,omitempty"`
	LastSyncError  string     `json:"lastSyncError,omitempty"`
	// RolledBackCommit is set while the application runs an earlier deployment.
	RolledBackCommit string `json:"rolledBackCommit,omitempty" jsonschema:"set while the application was rolled back to the deployment of this commit"`
	AutoSyncPaused   bool   `json:"autoSyncPaused,omitempty" jsonschema:"automatic syncs are paused after a rollback until the application is deployed again"`
}

type listApplicationsOutput struct {
	Applications []applicationSummary `json:"applications"`
}

type applicationInput struct {
	ApplicationId string `json:"applicationId" jsonschema:"the id of the application"`
}

type applicationEvent struct {
	Type          string     `json:"type"`
	Source        string     `json:"source"`
	Status        string     `json:"status"`
	ActorName     string     `json:"actorName,omitempty"`
	Commit        string     `json:"commit,omitempty"`
	CommitMessage string     `json:"commitMessage,omitempty"`
	Error         string     `json:"error,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	CompletedAt   *time.Time `json:"completedAt,omitempty"`
}

type applicationDetails struct {
	applicationSummary
	CommitMessage    string             `json:"commitMessage"`
	ComposeFile      string             `json:"composeFile"`
	Warnings         string             `json:"warnings,omitempty" jsonschema:"problems found in the compose file, as JSON"`
	RolledBackReason string             `json:"rolledBackReason,omitempty"`
	RecentEvents     []applicationEvent `json:"recentEvents" jsonschema:"the latest events, newest first"`
}

func (c *caller) listApplications(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, listApplicationsOutput, error) {
	applications, err := gorm.G[models.Application](db.DB).
		Scopes(c.perms.Scope(models.ResourceApplication, models.ActionRead, "id")).
		Preload("Repository", nil).
		Preload("Agent", nil).
		Order("created_at ASC").
		Find(ctx)
	if err != nil {
		Log.Error().Err(err).Msg("failed to list applications")
		return nil, listApplicationsOutput{}, errInternal
	}

	output := listApplicationsOutput{Applications: make([]applicationSummary, 0, len(applications))}
	for i := range applications {
		output.Applications = append(output.Applications, toApplicationSummary(&applications[i]))
	}
	return nil, output, nil
}

func (c *caller) getApplication(ctx context.Context, _ *mcp.CallToolRequest, in applicationInput) (*mcp.CallToolResult, applicationDetails, error) {
	application, err := c.loadApplication(ctx, in.ApplicationId, models.ActionRead)
	if err != nil {
		return nil, applicationDetails{}, err
	}

	events, err := gorm.G[models.ApplicationEvent](db.DB).
		Where("application_id = ?", application.Id).
		Order("created_at DESC, id DESC").
		Limit(recentEventLimit).
		Find(ctx)
	if err != nil {
		Log.Error().Err(err).Str("applicationId", application.Id).Msg("failed to list application events")
		return nil, applicationDetails{}, errInternal
	}

	details := applicationDetails{
		applicationSummary: toApplicationSummary(application),
		CommitMessage:      application.CommitMessage,
		ComposeFile:        application.ComposeFile.String(),
		RecentEvents:       make([]applicationEvent, 0, len(events)),
	}
	if application.Warnings != "[]" {
		details.Warnings = application.Warnings
	}
	if application.RolledBackReason != nil {
		details.RolledBackReason = *application.RolledBackReason
	}
	for i := range events {
		details.RecentEvents = append(details.RecentEvents, toApplicationEvent(&events[i]))
	}
	return nil, details, nil
}

// loadApplication loads an application with its repository and agent. An
// application the caller may not use is reported as not found, like one that
// doesn't exist, so its ID doesn't reveal it.
func (c *caller) loadApplication(ctx context.Context, id string, action models.PermissionAction) (*models.Application, error) {
	if id == "" || !c.perms.Can(models.ResourceApplication, action, id) {
		return nil, errApplicationNotFound
	}

	application, err := gorm.G[models.Application](db.DB).
		Preload("Repository", nil).
		Preload("Agent", nil).
		Where("id = ?", id).
		First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errApplicationNotFound
		}
		Log.Error().Err(err).Str("applicationId", id).Msg("failed to load application")
		return nil, errInternal
	}
	return &application, nil
}

func toApplicationSummary(app *models.Application) applicationSummary {
	summary := applicationSummary{
		Id:             app.Id,
		Name:           app.Name.String(),
		SyncStatus:     string(app.SyncStatus),
		HealthStatus:   string(app.HealthStatus),
		RepositoryId:   app.RepositoryId,
		RepositoryName: app.Repository.Name,
		Branch:         app.Branch,
		Path:           app.Path,
		Commit:         app.Commit,
		AgentId:        app.AgentId,
		AgentName:      app.Agent.Name.String(),
		LastSyncedAt:   app.LastSyncedAt,
		AutoSyncPaused: app.AutoSyncPaused,
	}
	if app.LastSyncError != nil {
		summary.LastSyncError = *app.LastSyncError
	}
	if app.RolledBackAt != nil && app.RolledBackCommit != nil {
		summary.RolledBackCommit = *app.RolledBackCommit
	}
	return summary
}

func toApplicationEvent(event *models.ApplicationEvent) applicationEvent {
	return applicationEvent{
		Type:          string(event.Type),
		Source:        string(event.Source),
		Status:        string(event.Status),
		ActorName:     deref(event.ActorName),
		Commit:        deref(event.CommitHash),
		CommitMessage: deref(event.CommitMessage),
		Error:         deref(event.ErrorMessage),
		CreatedAt:     event.CreatedAt,
		CompletedAt:   event.CompletedAt,
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
