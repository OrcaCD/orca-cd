package websocket

import (
	"context"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
)

func (h *Hub) ListContainers(ctx context.Context, agentID, applicationID string) (*messages.ContainerListResult, error) {
	return requestPending(ctx, h, h.pendingContainerLists, agentID, applicationID, func(requestID string) *messages.ServerMessage {
		return &messages.ServerMessage{
			Payload: &messages.ServerMessage_ContainerListRequest{
				ContainerListRequest: &messages.ContainerListRequest{
					RequestId:     requestID,
					ApplicationId: applicationID,
				},
			},
		}
	})
}

func (h *Hub) InspectContainer(ctx context.Context, agentID, applicationID, containerID string) (*messages.ContainerInspectResult, error) {
	return requestPending(ctx, h, h.pendingContainerInspects, agentID, applicationID, func(requestID string) *messages.ServerMessage {
		return &messages.ServerMessage{
			Payload: &messages.ServerMessage_ContainerInspectRequest{
				ContainerInspectRequest: &messages.ContainerInspectRequest{
					RequestId:     requestID,
					ApplicationId: applicationID,
					ContainerId:   containerID,
				},
			},
		}
	})
}

func (h *Hub) ResolveContainerListResult(agentID string, result *messages.ContainerListResult) bool {
	return resolvePending(h, h.pendingContainerLists, agentID, result.RequestId, result.ApplicationId, result)
}

func (h *Hub) ResolveContainerInspectResult(agentID string, result *messages.ContainerInspectResult) bool {
	return resolvePending(h, h.pendingContainerInspects, agentID, result.RequestId, result.ApplicationId, result)
}
