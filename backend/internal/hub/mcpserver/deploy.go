package mcpserver

import (
	"context"
	"errors"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/applications"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	application_deployer "github.com/OrcaCD/orca-cd/internal/hub/deployer"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/aymanbagabas/go-udiff"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"
)

var previewDeployTool = &mcp.Tool{
	Name: "preview_deploy",
	Description: "Shows what deploying an application would change: the diff between the compose file the agent runs and the one that will be deployed, and warnings like an offline agent. " +
		"Returns the previewId that deploy_application needs. Show the diff to the user before deploying.",
	Annotations: &mcp.ToolAnnotations{Title: "Preview deploy", ReadOnlyHint: true, OpenWorldHint: new(false)},
}

var deployApplicationTool = &mcp.Tool{
	Name: "deploy_application",
	Description: "Deploys the compose file of the application's commit to its agent, like the deploy button in OrcaCD. " +
		"Needs the previewId of preview_deploy and is refused when the application changed since the preview. The deploy runs in the background, check its result with get_application.",
	Annotations: &mcp.ToolAnnotations{Title: "Deploy application", DestructiveHint: new(true), OpenWorldHint: new(false)},
}

type previewDeployOutput struct {
	PreviewId       string    `json:"previewId" jsonschema:"pass to deploy_application to deploy exactly this preview"`
	ExpiresAt       time.Time `json:"expiresAt"`
	ApplicationId   string    `json:"applicationId"`
	ApplicationName string    `json:"applicationName"`
	AgentName       string    `json:"agentName"`
	AgentStatus     string    `json:"agentStatus"`
	Commit          string    `json:"commit" jsonschema:"the commit whose compose file is deployed"`
	CommitMessage   string    `json:"commitMessage"`
	RunningCommit   string    `json:"runningCommit,omitempty" jsonschema:"the commit of the deployment the agent runs"`
	Changed         bool      `json:"changed" jsonschema:"whether the compose file differs from the running one"`
	Diff            string    `json:"diff,omitempty" jsonschema:"unified diff from the running compose file to the deployed one"`
	Warnings        []string  `json:"warnings"`
}

type deployApplicationInput struct {
	ApplicationId string `json:"applicationId" jsonschema:"the id of the application"`
	PreviewId     string `json:"previewId" jsonschema:"the previewId returned by preview_deploy"`
}

type deployApplicationOutput struct {
	Message string `json:"message"`
}

func (c *caller) previewDeploy(ctx context.Context, _ *mcp.CallToolRequest, in applicationInput) (*mcp.CallToolResult, previewDeployOutput, error) {
	application, err := c.loadApplication(ctx, in.ApplicationId, models.ActionDeploy)
	if err != nil {
		return nil, previewDeployOutput{}, err
	}
	running, err := runningDeployment(ctx, application)
	if err != nil {
		Log.Error().Err(err).Str("applicationId", application.Id).Msg("failed to load running deployment")
		return nil, previewDeployOutput{}, errInternal
	}

	issuedAt := now()
	output := previewDeployOutput{
		PreviewId:       c.previewID(application, issuedAt),
		ExpiresAt:       issuedAt.Add(previewTTL).UTC().Truncate(time.Second),
		ApplicationId:   application.Id,
		ApplicationName: application.Name.String(),
		AgentName:       application.Agent.Name.String(),
		AgentStatus:     agentStatus(application.Agent.Status),
		Commit:          application.Commit,
		CommitMessage:   application.CommitMessage,
		Warnings:        []string{},
	}

	composeFile := application.ComposeFile.String()
	oldLabel, oldCompose := "/dev/null", ""
	if running == nil {
		output.Warnings = append(output.Warnings, "OrcaCD has no record of the deployment the agent runs, the diff shows the whole compose file.")
	} else {
		output.RunningCommit = running.Commit
		oldLabel, oldCompose = application.Path+"@"+running.Commit, running.ComposeFile.String()
	}
	// udiff.Unified would stop the hub with log.Fatalf if it failed.
	output.Diff, err = udiff.ToUnified(oldLabel, application.Path+"@"+application.Commit, oldCompose, udiff.Lines(oldCompose, composeFile), udiff.DefaultContextLines)
	if err != nil {
		Log.Error().Err(err).Str("applicationId", application.Id).Msg("failed to diff compose files")
		return nil, previewDeployOutput{}, errInternal
	}
	output.Changed = output.Diff != ""
	if running != nil && !output.Changed {
		output.Warnings = append(output.Warnings, "The compose file is the same as the running one, the deploy applies it again.")
	}

	if application.Agent.Status != models.AgentStatusOnline {
		output.Warnings = append(output.Warnings, "The agent "+application.Agent.Name.String()+" is not online, the deploy will fail.")
	}
	if application.SyncStatus == models.Syncing {
		output.Warnings = append(output.Warnings, "The application is being deployed right now.")
	}
	if application.RolledBackAt != nil {
		output.Warnings = append(output.Warnings, "The application was rolled back to an earlier deployment. Deploying runs the commit from Git again and resumes automatic syncs.")
	}
	if len(composeWarnings(application)) > 0 {
		output.Warnings = append(output.Warnings, "The compose file has warnings, get_application lists them.")
	}

	return nil, output, nil
}

func (c *caller) deployApplication(ctx context.Context, _ *mcp.CallToolRequest, in deployApplicationInput) (*mcp.CallToolResult, deployApplicationOutput, error) {
	application, err := c.loadApplication(ctx, in.ApplicationId, models.ActionDeploy)
	if err != nil {
		return nil, deployApplicationOutput{}, err
	}
	expiresAt, ok := c.validPreview(application, in.PreviewId)
	if !ok || !usedPreviews.use(in.PreviewId, expiresAt) {
		return nil, deployApplicationOutput{}, errPreviewMismatch
	}

	err = applications.Deploy(ctx, application, applications.SyncOrigin{
		Source:      models.ApplicationEventSourceManual,
		ActorUserID: &c.user.Id,
		ActorName:   &c.user.Name,
	})
	if err != nil {
		if errors.Is(err, application_deployer.ErrAgentOffline) {
			return nil, deployApplicationOutput{}, errors.New("agent is offline; cannot deploy application. Call preview_deploy again to retry once it is online")
		}
		Log.Error().Err(err).Str("applicationId", application.Id).Msg("failed to trigger application deploy")
		return nil, deployApplicationOutput{}, errInternal
	}

	return nil, deployApplicationOutput{Message: "deployment started"}, nil
}

// runningDeployment returns the deployment the agent runs, or nil if it isn't known.
func runningDeployment(ctx context.Context, application *models.Application) (*models.ApplicationDeployment, error) {
	if application.CurrentDeploymentId == nil {
		return nil, nil
	}
	deployment, err := gorm.G[models.ApplicationDeployment](db.DB).
		Where("id = ? AND application_id = ?", *application.CurrentDeploymentId, application.Id).
		First(ctx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &deployment, nil
}
