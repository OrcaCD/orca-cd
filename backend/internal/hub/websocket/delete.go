package websocket

import (
	"context"
	"errors"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/google/uuid"
)

// ErrAgentOffline is returned when a delete cannot be dispatched because the
// agent is not connected.
var ErrAgentOffline = errors.New("agent is not connected")

// RemoveApplication asks the agent to tear down an application and blocks until the
// agent reports the result or ctx is cancelled. The caller (e.g. the delete handler)
// uses the result to decide whether it is safe to drop its own record, so the hub
// never loses track of containers when the agent is offline or the removal fails.
func (h *Hub) RemoveApplication(ctx context.Context, agentID, applicationID, applicationName string) (*messages.DeleteResult, error) {
	requestID := uuid.NewString()
	ch := make(chan *messages.DeleteResult, 1)

	h.deleteMu.Lock()
	h.pendingDeletes[requestID] = ch
	h.deleteMu.Unlock()

	cleanup := func() {
		h.deleteMu.Lock()
		delete(h.pendingDeletes, requestID)
		h.deleteMu.Unlock()
	}

	msg := &messages.ServerMessage{
		Payload: &messages.ServerMessage_DeleteRequest{
			DeleteRequest: &messages.DeleteRequest{
				RequestId:       requestID,
				ApplicationId:   applicationID,
				ApplicationName: applicationName,
			},
		},
	}

	if !h.Send(agentID, msg) {
		cleanup()
		return nil, ErrAgentOffline
	}

	select {
	case result := <-ch:
		return result, nil
	case <-ctx.Done():
		cleanup()
		return nil, ctx.Err()
	}
}

// ResolveDeleteResult delivers an agent's DeleteResult to the waiting
// RemoveApplication call. Returns false if no caller is waiting for it.
func (h *Hub) ResolveDeleteResult(result *messages.DeleteResult) bool {
	h.deleteMu.Lock()
	ch, ok := h.pendingDeletes[result.RequestId]
	if ok {
		delete(h.pendingDeletes, result.RequestId)
	}
	h.deleteMu.Unlock()

	if !ok {
		return false
	}
	ch <- result
	return true
}
