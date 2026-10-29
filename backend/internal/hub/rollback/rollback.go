// Package rollback deploys an earlier deployment of an application again.
package rollback

import (
	"context"
	"errors"

	"github.com/OrcaCD/orca-cd/internal/hub/applicationevents"
	application_deployer "github.com/OrcaCD/orca-cd/internal/hub/deployer"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/shared/logger"
	"github.com/google/uuid"
)

var Log = logger.New("rollback", false)

var ErrDeployerNotInitialized = errors.New("application deployer not initialized")

// Origin identifies what triggered a rollback.
type Origin struct {
	Source      models.ApplicationEventSource
	ActorUserID *string
	ActorName   *string
}

// Trigger dispatches a rollback of app to target and records it as a rollback
// event. The agent's deploy result completes the event.
func Trigger(ctx context.Context, app *models.Application, target *models.ApplicationDeployment, origin Origin) error {
	deployer := application_deployer.DefaultApplicationDeployer
	if deployer == nil {
		return ErrDeployerNotInitialized
	}

	requestID := uuid.NewString()
	params := applicationevents.Params{
		ApplicationID: app.Id,
		RequestID:     &requestID,
		Type:          models.ApplicationEventRollback,
		Source:        origin.Source,
		ActorUserID:   origin.ActorUserID,
		ActorName:     origin.ActorName,
		DeploymentID:  &target.Id,
	}
	if target.Commit != "" {
		params.CommitHash = &target.Commit
	}
	if target.CommitMessage != "" {
		params.CommitMessage = &target.CommitMessage
	}
	if _, err := applicationevents.Start(ctx, params); err != nil {
		Log.Error().Err(err).Str("applicationId", app.Id).Msg("failed to record rollback event")
	}

	return deployer.TriggerRollback(ctx, app, target, requestID)
}
