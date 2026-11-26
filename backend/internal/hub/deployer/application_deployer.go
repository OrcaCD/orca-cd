package application_deployer

import (
	"context"
	"errors"
	"fmt"

	"github.com/OrcaCD/orca-cd/internal/hub/applicationevents"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/deployments"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/shared/deployfiles"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// messageSender sends a message to an agent. *websocket.Hub satisfies it; tests
// pass a mock so the deployer can be exercised without a real hub or agent.
type messageSender interface {
	Send(agentID string, msg *messages.ServerMessage) bool
	SendWithCapability(agentID string, msg *messages.ServerMessage, capability string) (sent, unsupported bool)
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

// ErrAgentFileSyncUnsupported is returned when the connected agent must be
// upgraded before it can apply a deployment's managed files.
var ErrAgentFileSyncUnsupported = errors.New("upgrade the agent to deploy additional files")

func (d *ApplicationDeployer) TriggerApplicationDeploy(ctx context.Context, app *models.Application, composeFile, requestID string) error {
	files, required, err := deploymentFiles(ctx, app, app.AdditionalFiles)
	if err != nil {
		d.recordDispatchFailure(ctx, app, requestID, err.Error())
		return err
	}
	if _, err := deployments.Start(ctx, app, composeFile, requestID); err != nil {
		d.log.Error().Err(err).Str("applicationId", app.Id).Msg("failed to record deployment")
		// A Compose-only deployment can still run without a rollback snapshot.
		if required {
			d.recordDispatchFailure(ctx, app, requestID, "could not record deployment files for rollback")
			return err
		}
	}

	err = d.dispatch(ctx, app, true, required, &messages.DeployRequest{
		RequestId:       requestID,
		ApplicationId:   app.Id,
		ApplicationName: app.Name.String(),
		ComposeFile:     composeFile,
		AdditionalFiles: files,
		DeleteOldImages: app.DeleteOldImages,
		ImageRetention:  d.imageRetention(ctx, app),
	})
	if errors.Is(err, ErrAgentOffline) || errors.Is(err, ErrAgentFileSyncUnsupported) {
		if _, discardErr := deployments.Discard(ctx, app.Id, requestID); discardErr != nil {
			d.log.Error().Err(discardErr).Str("applicationId", app.Id).Msg("failed to discard undispatched deployment")
		}
	}

	return err
}

// TriggerRollback deploys the Compose file, additional files, and exact images
// of an earlier deployment again.
func (d *ApplicationDeployer) TriggerRollback(ctx context.Context, app *models.Application, target *models.ApplicationDeployment, requestID string) error {
	files, required, err := deploymentFiles(ctx, app, target.AdditionalFiles)
	if err != nil {
		d.completeFailedEvent(ctx, requestID, app.Id, err.Error())
		return err
	}
	if err := deployments.Supersede(ctx, app.Id); err != nil {
		d.log.Error().Err(err).Str("applicationId", app.Id).Msg("failed to end the health check of the replaced deployment")
	}
	// The error of the deployment that is rolled back stays, so the rolled
	// back application shows why.
	return d.dispatch(ctx, app, false, required, &messages.DeployRequest{
		RequestId:       requestID,
		ApplicationId:   app.Id,
		ApplicationName: app.Name.String(),
		ComposeFile:     target.ComposeFile.String(),
		AdditionalFiles: files,
		DeleteOldImages: app.DeleteOldImages,
		PinnedImages:    deployments.ToServiceImages(target.Images),
		ImageRetention:  d.imageRetention(ctx, app),
	})
}

func deploymentFiles(ctx context.Context, app *models.Application, stored crypto.EncryptedString) (*messages.DeploymentFiles, bool, error) {
	files, err := models.DecodeDeploymentFiles(stored)
	if err != nil {
		return nil, false, err
	}
	required := len(files) != 0 || app.AdditionalFiles != "" || app.PendingFilesRevision != ""
	if !required && app.CurrentDeploymentId != nil {
		count, err := gorm.G[models.ApplicationDeployment](db.DB).
			Where("id = ? AND application_id = ? AND additional_files <> ''", *app.CurrentDeploymentId, app.Id).
			Count(ctx, "*")
		if err != nil {
			return nil, false, fmt.Errorf("check current deployment files: %w", err)
		}
		required = count != 0
	}
	return &messages.DeploymentFiles{Files: files}, required, nil
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

func (d *ApplicationDeployer) dispatch(ctx context.Context, app *models.Application, clearError, requireFiles bool, req *messages.DeployRequest) error {
	msg := &messages.ServerMessage{
		Payload: &messages.ServerMessage_DeployRequest{
			DeployRequest: req,
		},
	}

	var sent, unsupported bool
	if requireFiles {
		sent, unsupported = d.hub.SendWithCapability(app.AgentId, msg, deployfiles.Capability)
	} else {
		sent = d.hub.Send(app.AgentId, msg)
	}
	if unsupported {
		d.recordDispatchFailure(ctx, app, req.RequestId, ErrAgentFileSyncUnsupported.Error())
		return ErrAgentFileSyncUnsupported
	}
	if !sent {
		d.recordDispatchFailure(ctx, app, req.RequestId, "could not connect to agent: "+app.Agent.Name.String())
		return ErrAgentOffline
	}

	if err := markDeploymentInProgress(ctx, app.Id, clearError, d.log); err != nil {
		d.completeFailedEvent(ctx, req.RequestId, app.Id, fmt.Sprintf("failed to mark deployment in progress: %v", err))
		return err
	}
	return nil
}

func (d *ApplicationDeployer) recordDispatchFailure(ctx context.Context, app *models.Application, requestID, message string) {
	_ = updateApplicationStatus(ctx, app.Id, models.Application{
		SyncStatus:    models.OutOfSync,
		HealthStatus:  models.UnknownHealth,
		LastSyncError: &message,
	}, d.log)
	d.completeFailedEvent(ctx, requestID, app.Id, message)
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
	sse.PublishUpdate(sse.TopicApplications.For(applicationID))
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
