package websocket

import (
	"context"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/google/uuid"
)

type pendingRequest[T any] struct {
	agentID       string
	applicationID string
	client        *Client
	ch            chan T
}

func (h *Hub) abortPendingRequests(c *Client) {
	h.requestMu.Lock()
	defer h.requestMu.Unlock()
	abortPending(h.pendingEnvLists, c)
	abortPending(h.pendingEnvUpdates, c)
	abortPending(h.pendingContainerLists, c)
	abortPending(h.pendingContainerInspects, c)
}

func requestPending[T any](
	ctx context.Context,
	h *Hub,
	pending map[string]pendingRequest[T],
	agentID, applicationID string,
	build func(requestID string) *messages.ServerMessage,
) (T, error) {
	var zero T
	requestID := uuid.NewString()
	ch := make(chan T, 1)

	defer func() {
		h.requestMu.Lock()
		delete(pending, requestID)
		h.requestMu.Unlock()
	}()

	// Registered before queueing so the result cannot arrive first, and bound
	// to the client so a stale disconnect cannot abort it.
	register := func(c *Client) {
		h.requestMu.Lock()
		pending[requestID] = pendingRequest[T]{agentID: agentID, applicationID: applicationID, client: c, ch: ch}
		h.requestMu.Unlock()
	}
	if !h.send(agentID, build(requestID), register) {
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

func resolvePending[T any](h *Hub, pending map[string]pendingRequest[T], agentID, requestID, applicationID string, result T) bool {
	h.requestMu.Lock()
	entry, ok := pending[requestID]
	if ok && entry.agentID == agentID && entry.applicationID == applicationID {
		delete(pending, requestID)
	} else {
		ok = false
	}
	h.requestMu.Unlock()

	if !ok {
		return false
	}
	entry.ch <- result
	return true
}

func abortPending[T any](pending map[string]pendingRequest[T], c *Client) {
	for requestID, entry := range pending {
		if entry.client == c {
			delete(pending, requestID)
			close(entry.ch)
		}
	}
}
