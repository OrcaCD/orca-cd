package application_deployer

import (
	"context"
	"errors"
	"fmt"

	"github.com/OrcaCD/orca-cd/internal/hub/applicationevents"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/deployments"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// messageSender sends a message to an agent. *websocket.Hub satisfies it; tests
// pass a mock so the deployer can be exercised without a real hub or agent.
type messageSender interface {
	Send(agentID string, msg *messages.ServerMessage) bool
}

// ApplicationDeploymentManager dispatches a deploy to an agent. It is an interface
// so route handlers can be tested with a stub.
type ApplicationDeploymentManager interface {
	TriggerApplicationDeploy(ctx context.Context, app *models.Application, composeFile, requestID string) error
	TriggerRollback(ctx context.Context, app *models.Application, target *models.ApplicationDeployment, requestID string) error
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

// ErrAgentOffline is returned when a deploy cannot be dispatched because the
// agent is not connected.
var ErrAgentOffline = errors.New("agent is not connected")

func (d *ApplicationDeployer) TriggerApplicationDeploy(ctx context.Context, app *models.Application, composeFile, requestID string) error {
	// A deployment that can't be recorded can't be rolled back to, but still runs.
	if _, err := deployments.Start(ctx, app, composeFile, requestID); err != nil {
		d.log.Error().Err(err).Str("applicationId", app.Id).Msg("failed to record deployment")
	}

	err := d.dispatch(ctx, app, true, &messages.DeployRequest{
		RequestId:       requestID,
		ApplicationId:   app.Id,
		ApplicationName: app.Name.String(),
		ComposeFile:     composeFile,
		DeleteOldImages: app.DeleteOldImages,
		ImageRetention:  d.imageRetention(ctx, app),
	})
	if errors.Is(err, ErrAgentOffline) {
		if _, discardErr := deployments.Discard(ctx, app.Id, requestID); discardErr != nil {
			d.log.Error().Err(discardErr).Str("applicationId", app.Id).Msg("failed to discard undispatched deployment")
		}
	}
	return err
}

// TriggerRollback deploys the compose file and the exact images of an earlier
// deployment again.
func (d *ApplicationDeployer) TriggerRollback(ctx context.Context, app *models.Application, target *models.ApplicationDeployment, requestID string) error {
	if err := deployments.Supersede(ctx, app.Id); err != nil {
		d.log.Error().Err(err).Str("applicationId", app.Id).Msg("failed to end the health check of the replaced deployment")
	}
	// The error of the deployment that is rolled back stays, so the rolled
	// back application shows why.
	return d.dispatch(ctx, app, false, &messages.DeployRequest{
		RequestId:       requestID,
		ApplicationId:   app.Id,
		ApplicationName: app.Name.String(),
		ComposeFile:     target.ComposeFile.String(),
		DeleteOldImages: app.DeleteOldImages,
		PinnedImages:    deployments.ToServiceImages(target.Images),
		ImageRetention:  d.imageRetention(ctx, app),
	})
}

// imageRetention keeps the images a rollback needs when old images are
// deleted. Without it the agent deletes them, and a rollback pulls them again.
func (d *ApplicationDeployer) imageRetention(ctx context.Context, app *models.Application) *messages.ImageRetention {
	retention, err := deployments.ImageRetention(ctx, app)
	if err != nil {
		d.log.Error().Err(err).Str("applicationId", app.Id).Msg("failed to determine images to keep for rollbacks")
	}
	return retention
}

func (d *ApplicationDeployer) dispatch(ctx context.Context, app *models.Application, clearError bool, req *messages.DeployRequest) error {
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
		d.completeFailedEvent(ctx, req.RequestId, app.Id, errMsg)
		return ErrAgentOffline
	}

	if err := markDeploymentInProgress(ctx, app.Id, clearError, d.log); err != nil {
		d.completeFailedEvent(ctx, req.RequestId, app.Id, fmt.Sprintf("failed to mark deployment in progress: %v", err))
		return err
	}
	return nil
}

func (d *ApplicationDeployer) completeFailedEvent(ctx context.Context, requestID, applicationID, message string) {
	matched, err := applicationevents.Complete(ctx, requestID, applicationID, models.ApplicationEventFailed, &message)
	if err != nil {
		d.log.Error().Err(err).Str("applicationId", applicationID).Msg("failed to complete application event")
	} else if !matched {
		d.log.Debug().Str("applicationId", applicationID).Str("requestId", requestID).Msg("no running application event matched deploy failure")
	}
}

func updateApplicationStatus(ctx context.Context, applicationID string, updates models.Application, log *zerolog.Logger) error {
	_, err := gorm.G[models.Application](db.DB).
		Where("id = ?", applicationID).
		Updates(ctx, updates)
	if err != nil {
		log.Error().Err(err).Str("applicationId", applicationID).Msg("failed to update application status")
	}
	sse.PublishUpdate(sse.TopicApplications)
	return err
}

func markDeploymentInProgress(ctx context.Context, applicationID string, clearError bool, log *zerolog.Logger) error {
	updates := models.Application{
		SyncStatus:   models.Syncing,
		HealthStatus: models.UnknownHealth,
	}
	if clearError {
		// Non-nil pointer to "" forces the write.
		cleared := ""
		updates.LastSyncError = &cleared
	}
	return updateApplicationStatus(ctx, applicationID, updates, log)
}
