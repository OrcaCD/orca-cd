package websocket

import (
	"context"
	"errors"
	"sync"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
)

type DeployHandle struct {
	deployManager *DeployManager
	requestID     string
	outcome       chan deployOutcome
}

type deployOutcome struct {
	err    error
	result *messages.DeployResult
}

type pendingDeploy struct {
	agentID string
	outcome chan deployOutcome
}

var ErrAgentDisconnected = errors.New("agent disconnected before deployment completed")
var ErrDeployUnavailable = errors.New("agent is not connected or unable to receive deploy requests")

// Await waits for the deployment to complete or the context to be cancelled.
func (h *DeployHandle) Await(ctx context.Context) (*messages.DeployResult, error) {
	select {
	case outcome, ok := <-h.outcome:
		if !ok {
			return nil, ErrAgentDisconnected
		}
		return outcome.result, outcome.err
	case <-ctx.Done():
		h.Cancel()
		return nil, ctx.Err()
	}
}

// Cancel cancels the deployment.
func (h *DeployHandle) Cancel() {
	h.deployManager.CancelDeploy(h.requestID)
}

// TODO the deploys are currently stored in memory,
// which means they will be lost if the hub restarts.
// We should consider persisting them in the database
// if we want to support hub restarts without losing deploy state.

// DeployManager handles deployment state management.
type DeployManager struct {
	mu             sync.Mutex
	pendingDeploys map[string]pendingDeploy
}

// NewDeployManager creates a new deployment manager.
func NewDeployManager() *DeployManager {
	return &DeployManager{
		pendingDeploys: make(map[string]pendingDeploy),
	}
}

// StartDeploy initiates a new deployment and returns a handle to await its result.
func (dm *DeployManager) StartDeploy(agentID string, req *messages.DeployRequest) pendingDeploy {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	pending := pendingDeploy{
		agentID: agentID,
		outcome: make(chan deployOutcome, 1),
	}
	dm.pendingDeploys[req.RequestId] = pending
	return pending
}

// ResolveDeploy completes a pending deployment with a result.
func (dm *DeployManager) ResolveDeploy(result *messages.DeployResult) bool {
	dm.mu.Lock()
	pending, ok := dm.pendingDeploys[result.RequestId]
	if ok {
		delete(dm.pendingDeploys, result.RequestId)
	}
	dm.mu.Unlock()

	if !ok {
		return false
	}

	pending.outcome <- deployOutcome{result: result}
	close(pending.outcome)

	return true
}

// CancelDeploy cancels a pending deployment.
func (dm *DeployManager) CancelDeploy(requestID string) {
	dm.mu.Lock()
	pending, ok := dm.pendingDeploys[requestID]
	if ok {
		delete(dm.pendingDeploys, requestID)
	}
	dm.mu.Unlock()

	if ok {
		close(pending.outcome)
	}
}

// FailPendingDeploys fails all pending deployments for a given agent.
func (dm *DeployManager) FailPendingDeploys(agentID string, err error) {
	dm.mu.Lock()
	requestIDs := make([]string, 0)
	pending := make([]pendingDeploy, 0)
	for requestID, waiter := range dm.pendingDeploys {
		if waiter.agentID == agentID {
			requestIDs = append(requestIDs, requestID)
			pending = append(pending, waiter)
		}
	}
	for _, requestID := range requestIDs {
		delete(dm.pendingDeploys, requestID)
	}
	dm.mu.Unlock()

	for _, waiter := range pending {
		waiter.outcome <- deployOutcome{err: err}
		close(waiter.outcome)
	}
}
