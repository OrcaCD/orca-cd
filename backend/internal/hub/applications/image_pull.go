package applications

import (
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	hubws "github.com/OrcaCD/orca-cd/internal/hub/websocket"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/google/uuid"
)

// TriggerImagePull sends a PullImagesRequest to the agent for the given application.
// Returns false if the hub is not initialised, the agent is not connected, or the send buffer is full.
func TriggerImagePull(app *models.Application) bool {
	if hubws.DefaultHub == nil {
		return false
	}
	return hubws.DefaultHub.Send(app.AgentId, &messages.ServerMessage{
		Payload: &messages.ServerMessage_PullImagesRequest{
			PullImagesRequest: &messages.PullImagesRequest{
				RequestId:       uuid.NewString(),
				ApplicationId:   app.Id,
				ApplicationName: app.Name.String(),
			},
		},
	})
}
