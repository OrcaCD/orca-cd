package application_deployer

import (
	"context"
	"errors"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// messageSender sends a message to an agent. *websocket.Hub satisfies it; tests
// pass a mock so the deployer can be exercised without a real hub or agent.
type messageSender interface {
	Send(agentID string, msg *messages.ServerMessage) bool
}

const (
	applicationsSSEPath = "/api/v1/applications"
)

// ApplicationDeploymentManager dispatches a deploy to an agent. It is an interface
// so route handlers can be tested with a stub.
type ApplicationDeploymentManager interface {
	TriggerApplicationDeploy(ctx context.Context, app *models.Application, composeFile string) error
}

type ApplicationDeployer struct {
	hub messageSender
	log *zerolog.Logger
}

func NewApplicationDeployer(hub messageSender, log *zerolog.Logger) *ApplicationDeployer {
	return &ApplicationDeployer{
		hub: hub,
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

	if !d.hub.Send(app.AgentId, msg) {
		errMsg := "could not connect to agent: " + app.Agent.Name.String()
		_ = updateApplicationStatus(ctx, app.Id, models.Application{
			SyncStatus:    models.OutOfSync,
			HealthStatus:  models.UnknownHealth,
			LastSyncError: &errMsg,
		}, d.log)
		return errors.New("error: " + errMsg)
	}

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
	// Clear any previous error when a new deploy starts (non-nil ptr to "" forces the write).
	cleared := ""
	return updateApplicationStatus(ctx, applicationID, models.Application{
		SyncStatus:    models.Syncing,
		HealthStatus:  models.UnknownHealth,
		LastSyncError: &cleared,
	}, log)
}
