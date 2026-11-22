package websocket

import (
	"context"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
)

func (h *Hub) ListEnv(ctx context.Context, agentID, applicationID, applicationName string) (*messages.EnvListResult, error) {
	return requestPending(ctx, h, h.pendingEnvLists, agentID, applicationID, func(requestID string) *messages.ServerMessage {
		return &messages.ServerMessage{
			Payload: &messages.ServerMessage_EnvListRequest{
				EnvListRequest: &messages.EnvListRequest{
					RequestId:       requestID,
					ApplicationId:   applicationID,
					ApplicationName: applicationName,
				},
			},
		}
	})
}

func (h *Hub) UpdateEnv(ctx context.Context, agentID, applicationID, applicationName string, files []*messages.EnvFileChanges) (*messages.EnvUpdateResult, error) {
	return requestPending(ctx, h, h.pendingEnvUpdates, agentID, applicationID, func(requestID string) *messages.ServerMessage {
		return &messages.ServerMessage{
			Payload: &messages.ServerMessage_EnvUpdateRequest{
				EnvUpdateRequest: &messages.EnvUpdateRequest{
					RequestId:       requestID,
					ApplicationId:   applicationID,
					ApplicationName: applicationName,
					Files:           files,
				},
			},
		}
	})
}

func (h *Hub) ResolveEnvListResult(agentID string, result *messages.EnvListResult) bool {
	return resolvePending(h, h.pendingEnvLists, agentID, result.RequestId, result.ApplicationId, result)
}

func (h *Hub) ResolveEnvUpdateResult(agentID string, result *messages.EnvUpdateResult) bool {
	return resolvePending(h, h.pendingEnvUpdates, agentID, result.RequestId, result.ApplicationId, result)
}
