package websocket

import (
	"context"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/google/uuid"
)

type pendingImageStatus struct {
	agentID string
	ch      chan *messages.ImageStatusResult
}

// RequestImageStatus asks the agent which image versions an application runs
// and blocks until the agent answers or ctx is cancelled. With checkLatest the
// agent also queries the registry for the digests a pull would fetch.
func (h *Hub) RequestImageStatus(ctx context.Context, agentID, applicationID, applicationName string, checkLatest bool) (*messages.ImageStatusResult, error) {
	requestID := uuid.NewString()
	ch := make(chan *messages.ImageStatusResult, 1)

	h.imageStatusMu.Lock()
	h.pendingImageStatus[requestID] = pendingImageStatus{agentID: agentID, ch: ch}
	h.imageStatusMu.Unlock()

	defer func() {
		h.imageStatusMu.Lock()
		delete(h.pendingImageStatus, requestID)
		h.imageStatusMu.Unlock()
	}()

	msg := &messages.ServerMessage{
		Payload: &messages.ServerMessage_ImageStatusRequest{
			ImageStatusRequest: &messages.ImageStatusRequest{
				RequestId:       requestID,
				ApplicationId:   applicationID,
				ApplicationName: applicationName,
				CheckLatest:     checkLatest,
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

// ResolveImageStatusResult delivers an agent's ImageStatusResult to the waiting
// RequestImageStatus call. Results are only accepted from the agent the request
// was sent to. Returns false if no caller is waiting for it.
func (h *Hub) ResolveImageStatusResult(agentID string, result *messages.ImageStatusResult) bool {
	h.imageStatusMu.Lock()
	pending, ok := h.pendingImageStatus[result.RequestId]
	if ok && pending.agentID == agentID {
		delete(h.pendingImageStatus, result.RequestId)
	} else {
		ok = false
	}
	h.imageStatusMu.Unlock()

	if !ok {
		return false
	}
	pending.ch <- result
	return true
}
