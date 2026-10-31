package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	agentdocker "github.com/OrcaCD/orca-cd/internal/agent/docker"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/shared/wscrypto"
)

type stubEnvDeployer struct {
	stubDeployer

	mu          sync.Mutex
	calls       []string
	files       []*messages.EnvFile
	listErr     error
	updateErr   error
	redeployErr error
	updateStart chan struct{}
}

func (d *stubEnvDeployer) record(call string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, call)
}

func (d *stubEnvDeployer) recordedCalls() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.calls...)
}

func (d *stubEnvDeployer) ListEnv(_ context.Context, appName string) ([]*messages.EnvFile, error) {
	d.record("list " + appName)
	return d.files, d.listErr
}

func (d *stubEnvDeployer) UpdateEnv(_ context.Context, appName string, files []*messages.EnvFileChanges) error {
	if d.updateStart != nil {
		d.updateStart <- struct{}{}
	}
	d.record(fmt.Sprintf("update %s %d", appName, len(files)))
	return d.updateErr
}

func (d *stubEnvDeployer) Redeploy(_ context.Context, appID, appName string) error {
	d.record("redeploy " + appID + " " + appName)
	return d.redeployErr
}

func receiveMessage(t *testing.T, sender *stubSender) *messages.ClientMessage {
	t.Helper()
	select {
	case msg := <-sender.sent:
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a message")
		return nil
	}
}

func TestExecuteEnvList_Success(t *testing.T) {
	sender := &stubSender{sent: make(chan *messages.ClientMessage, 1)}
	deployer := &stubEnvDeployer{files: []*messages.EnvFile{{
		Path:     ".env",
		Revision: "rev-1",
		Entries:  []*messages.EnvEntry{{Key: "LOG_LEVEL", Value: "debug"}, {Key: "TOKEN", IsSecret: true, IsSet: true}},
	}}}

	executeEnvList(t.Context(), sender, deployer, &messages.EnvListRequest{
		RequestId: "req-1", ApplicationId: "app-1", ApplicationName: "billing",
	})

	result := receiveMessage(t, sender).GetEnvListResult()
	if result == nil {
		t.Fatal("expected EnvListResult payload")
	}
	if !result.Success || result.RequestId != "req-1" || result.ApplicationId != "app-1" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(result.EnvFiles) != 1 || result.EnvFiles[0].Revision != "rev-1" {
		t.Fatalf("unexpected env files: %+v", result.EnvFiles)
	}
	if got := deployer.recordedCalls(); len(got) != 1 || got[0] != "list billing" {
		t.Fatalf("unexpected calls: %v", got)
	}
}

func TestExecuteEnvList_Error(t *testing.T) {
	sender := &stubSender{sent: make(chan *messages.ClientMessage, 1)}
	deployer := &stubEnvDeployer{listErr: errors.New("compose file missing")}

	executeEnvList(t.Context(), sender, deployer, &messages.EnvListRequest{RequestId: "req-1", ApplicationId: "app-1"})

	result := receiveMessage(t, sender).GetEnvListResult()
	if result.Success || result.ErrorMessage != "compose file missing" || len(result.EnvFiles) != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestExecuteEnvList_UnsupportedDeployer(t *testing.T) {
	for name, deployer := range map[string]deployExecutor{"nil": nil, "without env support": &stubDeployer{}} {
		t.Run(name, func(t *testing.T) {
			sender := &stubSender{sent: make(chan *messages.ClientMessage, 1)}

			executeEnvList(t.Context(), sender, deployer, &messages.EnvListRequest{RequestId: "req-1", ApplicationId: "app-1"})

			result := receiveMessage(t, sender).GetEnvListResult()
			if result.Success || result.ErrorMessage == "" {
				t.Fatalf("expected an error result, got %+v", result)
			}
		})
	}
}

func TestExecuteEnvUpdate_SavesAndRedeploysOnce(t *testing.T) {
	sender := &stubSender{sent: make(chan *messages.ClientMessage, 1)}
	deployer := &stubEnvDeployer{}

	executeEnvUpdate(t.Context(), sender, deployer, &messages.EnvUpdateRequest{
		RequestId: "req-1", ApplicationId: "app-1", ApplicationName: "billing",
		Files: []*messages.EnvFileChanges{{Path: ".env"}, {Path: "db.env"}},
	})

	result := receiveMessage(t, sender).GetEnvUpdateResult()
	if result == nil {
		t.Fatal("expected EnvUpdateResult payload")
	}
	if !result.Success || result.Conflict || result.RequestId != "req-1" || result.ApplicationId != "app-1" {
		t.Fatalf("unexpected result: %+v", result)
	}
	want := []string{"update billing 2", "redeploy app-1 billing"}
	if got := deployer.recordedCalls(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("expected calls %v, got %v", want, got)
	}
}

func TestExecuteEnvUpdate_ConflictDoesNotRedeploy(t *testing.T) {
	sender := &stubSender{sent: make(chan *messages.ClientMessage, 1)}
	deployer := &stubEnvDeployer{updateErr: fmt.Errorf("wrapped: %w", agentdocker.ErrEnvConflict)}

	executeEnvUpdate(t.Context(), sender, deployer, &messages.EnvUpdateRequest{RequestId: "req-1", ApplicationId: "app-1"})

	result := receiveMessage(t, sender).GetEnvUpdateResult()
	if result.Success || !result.Conflict || result.ErrorMessage == "" {
		t.Fatalf("expected a conflict, got %+v", result)
	}
	for _, call := range deployer.recordedCalls() {
		if strings.HasPrefix(call, "redeploy") {
			t.Fatal("expected no redeploy after a conflict")
		}
	}
}

func TestExecuteEnvUpdate_UpdateErrorDoesNotRedeploy(t *testing.T) {
	sender := &stubSender{sent: make(chan *messages.ClientMessage, 1)}
	deployer := &stubEnvDeployer{updateErr: errors.New("env file \".env\" is not declared in the compose file")}

	executeEnvUpdate(t.Context(), sender, deployer, &messages.EnvUpdateRequest{RequestId: "req-1", ApplicationId: "app-1"})

	result := receiveMessage(t, sender).GetEnvUpdateResult()
	if result.Success || result.Conflict || result.ErrorMessage != deployer.updateErr.Error() {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got := deployer.recordedCalls(); len(got) != 1 {
		t.Fatalf("expected no redeploy, got calls %v", got)
	}
}

func TestExecuteEnvUpdate_RedeployFailureIsReported(t *testing.T) {
	sender := &stubSender{sent: make(chan *messages.ClientMessage, 1)}
	deployer := &stubEnvDeployer{redeployErr: errors.New("compose up failed")}

	executeEnvUpdate(t.Context(), sender, deployer, &messages.EnvUpdateRequest{RequestId: "req-1", ApplicationId: "app-1"})

	result := receiveMessage(t, sender).GetEnvUpdateResult()
	if result.Success || result.Conflict {
		t.Fatalf("expected a failure, got %+v", result)
	}
	if !strings.Contains(result.ErrorMessage, "saved") || !strings.Contains(result.ErrorMessage, "compose up failed") {
		t.Fatalf("expected the error to say the changes were saved and why the redeploy failed, got %q", result.ErrorMessage)
	}
}

func TestExecuteEnvUpdate_UnsupportedDeployer(t *testing.T) {
	sender := &stubSender{sent: make(chan *messages.ClientMessage, 1)}

	executeEnvUpdate(t.Context(), sender, &stubDeployer{}, &messages.EnvUpdateRequest{RequestId: "req-1", ApplicationId: "app-1"})

	result := receiveMessage(t, sender).GetEnvUpdateResult()
	if result.Success || result.ErrorMessage == "" {
		t.Fatalf("expected an error result, got %+v", result)
	}
}

func TestExecuteEnvUpdate_SerializesWithDeploy(t *testing.T) {
	sender := &stubSender{sent: make(chan *messages.ClientMessage, 2)}
	deployer := &stubEnvDeployer{
		updateStart: make(chan struct{}, 1),
	}
	deployer.reqCh = make(chan agentdocker.DeployRequest, 1)
	deployer.block = make(chan struct{})

	go executeDeployment(t.Context(), sender, deployer, &messages.DeployRequest{
		RequestId: "deploy-1", ApplicationId: "app-locked", ApplicationName: "billing", ComposeFile: "services: {}\n",
	})
	select {
	case <-deployer.reqCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the deploy to start")
	}

	go executeEnvUpdate(t.Context(), sender, deployer, &messages.EnvUpdateRequest{
		RequestId: "env-1", ApplicationId: "app-locked", ApplicationName: "billing",
	})
	select {
	case <-deployer.updateStart:
		t.Fatal("env update started while a deploy of the same application was running")
	case <-time.After(100 * time.Millisecond):
	}

	close(deployer.block)
	select {
	case <-deployer.updateStart:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the env update after the deploy finished")
	}

	for range 2 {
		receiveMessage(t, sender)
	}
}

func TestHandleServerMessage_EnvRequests(t *testing.T) {
	session, err := wscrypto.NewSession(make([]byte, 32), wscrypto.RoleAgent)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	hub := newHubTestSession(t)
	sender := &stubSender{sent: make(chan *messages.ClientMessage, 2)}
	deployer := &stubEnvDeployer{}

	for _, payload := range []*messages.ServerMessage{
		{Payload: &messages.ServerMessage_EnvListRequest{EnvListRequest: &messages.EnvListRequest{RequestId: "list-1", ApplicationId: "app-h", ApplicationName: "billing"}}},
		{Payload: &messages.ServerMessage_EnvUpdateRequest{EnvUpdateRequest: &messages.EnvUpdateRequest{RequestId: "update-1", ApplicationId: "app-h", ApplicationName: "billing"}}},
	} {
		env, err := hub.Encrypt(payload)
		if err != nil {
			t.Fatalf("Encrypt: %v", err)
		}
		handleServerMessage(t.Context(), &messages.ServerMessage{
			Payload: &messages.ServerMessage_EncryptedPayload{EncryptedPayload: env},
		}, session, sender, deployer, nil)
	}

	got := map[string]bool{}
	for range 2 {
		msg := receiveMessage(t, sender)
		if r := msg.GetEnvListResult(); r != nil {
			got[r.RequestId] = r.Success
		}
		if r := msg.GetEnvUpdateResult(); r != nil {
			got[r.RequestId] = r.Success
		}
	}
	if !got["list-1"] || !got["update-1"] {
		t.Fatalf("expected both requests to succeed, got %v", got)
	}
}
