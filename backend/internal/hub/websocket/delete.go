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

type pendingDelete struct {
	agentID       string
	applicationID string
	ch            chan *messages.DeleteResult
}

// RemoveApplication asks the agent to tear down an application and blocks until the
// agent reports the result or ctx is cancelled. The caller (e.g. the delete handler)
// uses the result to decide whether it is safe to drop its own record, so the hub
// never loses track of containers when the agent is offline or the removal fails.
func (h *Hub) RemoveApplication(ctx context.Context, agentID, applicationID, applicationName string) (*messages.DeleteResult, error) {
	requestID := uuid.NewString()
	ch := make(chan *messages.DeleteResult, 1)

	h.deleteMu.Lock()
	h.pendingDeletes[requestID] = pendingDelete{agentID: agentID, applicationID: applicationID, ch: ch}
	h.deleteMu.Unlock()

	defer func() {
		h.deleteMu.Lock()
		delete(h.pendingDeletes, requestID)
		h.deleteMu.Unlock()
	}()

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
		return nil, ErrAgentOffline
	}

	select {
	case result := <-ch:
		return result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ResolveDeleteResult delivers an agent's DeleteResult to the waiting
// RemoveApplication call. Returns false if no caller is waiting for it.
func (h *Hub) ResolveDeleteResult(agentID string, result *messages.DeleteResult) bool {
	h.deleteMu.Lock()
	pending, ok := h.pendingDeletes[result.RequestId]
	if ok && pending.agentID == agentID && pending.applicationID == result.ApplicationId {
		delete(h.pendingDeletes, result.RequestId)
	} else {
		ok = false
	}
	h.deleteMu.Unlock()

	if !ok {
		return false
	}
	pending.ch <- result
	return true
}
