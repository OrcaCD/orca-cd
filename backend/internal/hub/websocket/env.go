package websocket

import (
	"context"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/google/uuid"
)

type pendingEnv[T any] struct {
	agentID       string
	applicationID string
	ch            chan T
}

func (h *Hub) ListEnv(ctx context.Context, agentID, applicationID, applicationName string) (*messages.EnvListResult, error) {
	return requestEnv(ctx, h, h.pendingEnvLists, agentID, applicationID, func(requestID string) *messages.ServerMessage {
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
	return requestEnv(ctx, h, h.pendingEnvUpdates, agentID, applicationID, func(requestID string) *messages.ServerMessage {
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
	return resolveEnv(h, h.pendingEnvLists, agentID, result.RequestId, result.ApplicationId, result)
}

func (h *Hub) ResolveEnvUpdateResult(agentID string, result *messages.EnvUpdateResult) bool {
	return resolveEnv(h, h.pendingEnvUpdates, agentID, result.RequestId, result.ApplicationId, result)
}

func (h *Hub) abortPendingEnv(agentID string) {
	h.envMu.Lock()
	defer h.envMu.Unlock()
	abortEnv(h.pendingEnvLists, agentID)
	abortEnv(h.pendingEnvUpdates, agentID)
}

func requestEnv[T any](
	ctx context.Context,
	h *Hub,
	pending map[string]pendingEnv[T],
	agentID, applicationID string,
	build func(requestID string) *messages.ServerMessage,
) (T, error) {
	var zero T
	requestID := uuid.NewString()
	ch := make(chan T, 1)

	h.envMu.Lock()
	pending[requestID] = pendingEnv[T]{agentID: agentID, applicationID: applicationID, ch: ch}
	h.envMu.Unlock()

	defer func() {
		h.envMu.Lock()
		delete(pending, requestID)
		h.envMu.Unlock()
	}()

	if !h.Send(agentID, build(requestID)) {
		return zero, ErrAgentOffline
	}

	select {
	case result, ok := <-ch:
		if !ok {
			return zero, ErrAgentOffline
		}
		return result, nil
	case <-ctx.Done():
		return zero, ctx.Err()
	}
}

func resolveEnv[T any](h *Hub, pending map[string]pendingEnv[T], agentID, requestID, applicationID string, result T) bool {
	h.envMu.Lock()
	entry, ok := pending[requestID]
	if ok && entry.agentID == agentID && entry.applicationID == applicationID {
		delete(pending, requestID)
	} else {
		ok = false
	}
	h.envMu.Unlock()

	if !ok {
		return false
	}
	entry.ch <- result
	return true
}

func abortEnv[T any](pending map[string]pendingEnv[T], agentID string) {
	for requestID, entry := range pending {
		if entry.agentID == agentID {
			delete(pending, requestID)
			close(entry.ch)
		}
	}
}
