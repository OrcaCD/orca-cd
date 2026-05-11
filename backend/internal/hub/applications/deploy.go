package applications

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	hubws "github.com/OrcaCD/orca-cd/internal/hub/websocket"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

const (
	applicationsSSEPath = "/api/v1/applications"
	manualDeployTimeout = 5 * time.Minute
)

var ErrAgentUnavailable = errors.New("agent is not connected")

type DeploymentHandle interface {
	Await(ctx context.Context) (*messages.DeployResult, error)
	Cancel()
}

type DeploymentManager interface {
	StartDeploy(app *models.Application, composeFile string) (DeploymentHandle, error)
	DeployAndWait(ctx context.Context, app *models.Application, composeFile string) (*messages.DeployResult, error)
	TrackManualDeploy(app models.Application, handle DeploymentHandle)
}

type deployTransport interface {
	StartDeploy(agentID string, req *messages.DeployRequest) (*hubws.DeployHandle, error)
}

type websocketTransport struct {
	hub *hubws.Hub
}

func (t websocketTransport) StartDeploy(agentID string, req *messages.DeployRequest) (*hubws.DeployHandle, error) {
	return t.hub.StartDeploy(agentID, req)
}

type Deployer struct {
	log       *zerolog.Logger
	transport deployTransport
}

var DefaultDeployer DeploymentManager

func NewDeployer(hub *hubws.Hub, log *zerolog.Logger) *Deployer {
	return &Deployer{
		log:       log,
		transport: websocketTransport{hub: hub},
	}
}

func (d *Deployer) StartDeploy(app *models.Application, composeFile string) (DeploymentHandle, error) {
	if app == nil {
		return nil, errors.New("application is required")
	}
	if app.AgentId == "" {
		return nil, errors.New("application is missing agent id")
	}
	if composeFile == "" {
		return nil, errors.New("application compose file is empty")
	}

	handle, err := d.transport.StartDeploy(app.AgentId, &messages.DeployRequest{
		RequestId:       uuid.NewString(),
		ApplicationId:   app.Id,
		ApplicationName: app.Name.String(),
		ComposeFile:     composeFile,
	})
	if err != nil {
		if errors.Is(err, hubws.ErrDeployUnavailable) {
			return nil, ErrAgentUnavailable
		}
		return nil, fmt.Errorf("start deploy: %w", err)
	}

	return handle, nil
}

func (d *Deployer) DeployAndWait(ctx context.Context, app *models.Application, composeFile string) (*messages.DeployResult, error) {
	handle, err := d.StartDeploy(app, composeFile)
	if err != nil {
		return nil, err
	}

	result, err := handle.Await(ctx)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (d *Deployer) TrackManualDeploy(app models.Application, handle DeploymentHandle) {
	go d.trackManualDeploy(app, handle)
}

func (d *Deployer) trackManualDeploy(app models.Application, handle DeploymentHandle) {
	ctx, cancel := context.WithTimeout(context.Background(), manualDeployTimeout)
	defer cancel()

	result, err := handle.Await(ctx)
	if err != nil {
		d.log.Error().Err(err).Str("applicationId", app.Id).Msg("manual deployment failed")
		markDeploymentTransportError(context.Background(), app.Id, d.log)
		return
	}

	if result == nil {
		d.log.Error().Str("applicationId", app.Id).Msg("manual deployment finished without a result")
		markDeploymentTransportError(context.Background(), app.Id, d.log)
		return
	}

	if !result.Success {
		d.log.Error().
			Str("applicationId", app.Id).
			Str("request_id", result.RequestId).
			Str("error", result.ErrorMessage).
			Msg("manual deployment failed on agent")
		markDeploymentExecutionFailure(context.Background(), app.Id, d.log)
		return
	}

	markDeploymentSuccess(context.Background(), app.Id, nil, d.log)
}

func markDeploymentSuccess(ctx context.Context, applicationID string, updateFn func(*models.Application), log *zerolog.Logger) {
	now := time.Now()
	updates := models.Application{
		SyncStatus:   models.Synced,
		HealthStatus: models.Healthy,
		LastSyncedAt: &now,
	}

	if updateFn != nil {
		updateFn(&updates)
	}

	if _, err := gorm.G[models.Application](db.DB).
		Where("id = ?", applicationID).
		Updates(ctx, updates); err != nil {
		log.Error().Err(err).Str("applicationId", applicationID).Msg("failed to update application after deployment")
		return
	}

	sse.PublishUpdate(applicationsSSEPath)
}

func markDeploymentExecutionFailure(ctx context.Context, applicationID string, log *zerolog.Logger) {
	if _, err := gorm.G[models.Application](db.DB).
		Where("id = ?", applicationID).
		Updates(ctx, models.Application{
			SyncStatus:   models.OutOfSync,
			HealthStatus: models.Unhealthy,
		}); err != nil {
		log.Error().Err(err).Str("applicationId", applicationID).Msg("failed to mark application deployment failure")
		return
	}

	sse.PublishUpdate(applicationsSSEPath)
}

func markDeploymentTransportError(ctx context.Context, applicationID string, log *zerolog.Logger) {
	if _, err := gorm.G[models.Application](db.DB).
		Where("id = ?", applicationID).
		Updates(ctx, models.Application{
			SyncStatus: models.OutOfSync,
		}); err != nil {
		log.Error().Err(err).Str("applicationId", applicationID).Msg("failed to mark application deployment transport error")
		return
	}

	sse.PublishUpdate(applicationsSSEPath)
}
