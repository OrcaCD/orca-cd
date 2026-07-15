package applications

import (
	"context"

	"github.com/OrcaCD/orca-cd/internal/hub/applicationevents"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	hubws "github.com/OrcaCD/orca-cd/internal/hub/websocket"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/google/uuid"
)

// TriggerImagePull sends a PullImagesRequest to the agent for the given application
// and records a correlated image_update history event for the explicit trigger.
// Returns false if the hub is not initialized, the agent is not connected, or the send buffer is full.
func TriggerImagePull(app *models.Application, source models.ApplicationEventSource) bool {
	ctx := context.Background()
	requestID := uuid.NewString()
	if _, err := applicationevents.Start(ctx, applicationevents.Params{
		ApplicationID: app.Id,
		RequestID:     &requestID,
		Type:          models.ApplicationEventImageUpdate,
		Source:        source,
	}); err != nil {
		Log.Error().Err(err).Str("applicationId", app.Id).Msg("failed to record image update event")
	}

	if hubws.DefaultHub == nil {
		failImagePullEvent(ctx, requestID, app.Id, "hub not initialized")
		return false
	}
	sent := hubws.DefaultHub.Send(app.AgentId, &messages.ServerMessage{
		Payload: &messages.ServerMessage_PullImagesRequest{
			PullImagesRequest: &messages.PullImagesRequest{
				RequestId:       requestID,
				ApplicationId:   app.Id,
				ApplicationName: app.Name.String(),
			},
		},
	})
	if !sent {
		failImagePullEvent(ctx, requestID, app.Id, "agent is not connected")
		return false
	}

	// Image-only updates redeploy affected services just like compose updates do.
	// Persist the in-progress state and notify the UI after the request is accepted.
	cleared := ""
	_ = updateApplicationStatus(ctx, app.Id, models.Application{
		SyncStatus:    models.Syncing,
		HealthStatus:  models.UnknownHealth,
		LastSyncError: &cleared,
	}, &Log)
	return true
}

func failImagePullEvent(ctx context.Context, requestID, applicationID, message string) {
	if _, err := applicationevents.Complete(ctx, requestID, applicationID, models.ApplicationEventFailed, &message); err != nil {
		Log.Error().Err(err).Str("applicationId", applicationID).Msg("failed to complete image update event")
	}
}
