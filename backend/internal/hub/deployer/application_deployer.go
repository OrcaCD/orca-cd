package application_deployer

import (
	"context"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/OrcaCD/orca-cd/internal/hub/websocket"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

const (
	applicationsSSEPath = "/api/v1/applications"
)

// ApplicationDeploymentManager dispatches a deploy to an agent. It is an interface
// so route handlers can be tested with a stub.
type ApplicationDeploymentManager interface {
	TriggerApplicationDeploy(ctx context.Context, app *models.Application, composeFile string) error
}

type ApplicationDeployer struct {
	log *zerolog.Logger
}

func NewApplicationDeployer(log *zerolog.Logger) *ApplicationDeployer {
	return &ApplicationDeployer{
		log: log,
	}
}

var DefaultApplicationDeployer ApplicationDeploymentManager

func (d *ApplicationDeployer) TriggerApplicationDeploy(ctx context.Context, app *models.Application, composeFile string) error {

	req := &messages.DeployRequest{
		RequestId:       uuid.NewString(),
		ApplicationId:   app.Id,
		ApplicationName: app.Name.String(),
		ComposeFile:     composeFile,
	}

	msg := &messages.ServerMessage{
		Payload: &messages.ServerMessage_DeployRequest{
			DeployRequest: req,
		},
	}

	// TODO Handle the case where the agent is not connected to the hub
	websocket.DefaultHub.Send(app.AgentId, msg)

	return markDeploymentInProgress(ctx, app.Id, d.log)
}

func updateApplicationStatus(ctx context.Context, applicationID string, updates models.Application, log *zerolog.Logger) error {
	_, err := gorm.G[models.Application](db.DB).
		Where("id = ?", applicationID).
		Updates(ctx, updates)
	if err != nil {
		log.Error().Err(err).Str("applicationId", applicationID).Msg("failed to update application status")
	}
	sse.PublishUpdate(applicationsSSEPath)
	return err
}

func markDeploymentInProgress(ctx context.Context, applicationID string, log *zerolog.Logger) error {
	return updateApplicationStatus(ctx, applicationID, models.Application{
		SyncStatus:   models.Syncing,
		HealthStatus: models.UnknownHealth,
	}, log)
}
