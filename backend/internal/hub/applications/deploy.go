package applications

import (
	"context"
	"errors"

	"github.com/OrcaCD/orca-cd/internal/hub/applicationevents"
	application_deployer "github.com/OrcaCD/orca-cd/internal/hub/deployer"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/google/uuid"
)

var ErrDeployerNotInitialized = errors.New("application deployer not initialized")

// Deploy deploys the stored compose file of an application again, like the
// deploy button does. It returns application_deployer.ErrAgentOffline when the
// agent isn't connected. The application must have its agent loaded.
func Deploy(ctx context.Context, application *models.Application, origin SyncOrigin) error {
	deployer := application_deployer.DefaultApplicationDeployer
	if deployer == nil {
		return ErrDeployerNotInitialized
	}

	requestID := uuid.NewString()
	if _, err := applicationevents.Start(ctx, applicationevents.Params{
		ApplicationID: application.Id,
		RequestID:     &requestID,
		Type:          models.ApplicationEventDeployment,
		Source:        origin.Source,
		ActorUserID:   origin.ActorUserID,
		ActorName:     origin.ActorName,
	}); err != nil {
		Log.Error().Err(err).Str("applicationId", application.Id).Msg("failed to record deployment event")
	}

	if err := deployer.TriggerApplicationDeploy(ctx, application, application.ComposeFile.String(), requestID); err != nil {
		return err
	}

	sse.PublishUpdate(sse.TopicApplications.For(application.Id))
	return nil
}
