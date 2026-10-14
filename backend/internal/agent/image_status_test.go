package agent

import (
	"context"
	"errors"
	"testing"

	agentdocker "github.com/OrcaCD/orca-cd/internal/agent/docker"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
)

type stubImageStatusProvider struct {
	statuses    []agentdocker.ServiceImageStatus
	err         error
	checkLatest bool
}

func (p *stubImageStatusProvider) ImageStatus(_ context.Context, _, _ string, checkLatest bool) ([]agentdocker.ServiceImageStatus, error) {
	p.checkLatest = checkLatest
	return p.statuses, p.err
}

func imageStatusResult(t *testing.T, sender *stubSender) *messages.ImageStatusResult {
	t.Helper()
	msg := <-sender.sent
	result := msg.GetImageStatusResult()
	if result == nil {
		t.Fatalf("expected ImageStatusResult, got %T", msg.Payload)
	}
	return result
}

func TestExecuteImageStatus_ReportsImages(t *testing.T) {
	sender := &stubSender{sent: make(chan *messages.ClientMessage, 1)}
	provider := &stubImageStatusProvider{statuses: []agentdocker.ServiceImageStatus{{
		Service:         "web",
		Image:           "nginx:latest",
		CurrentDigest:   "sha256:old",
		CurrentVersion:  "1.27.0",
		LatestDigest:    "sha256:new",
		UpdateAvailable: true,
	}}}

	executeImageStatus(t.Context(), sender, provider, &messages.ImageStatusRequest{
		RequestId:     "req-1",
		ApplicationId: "app-1",
		CheckLatest:   true,
	})

	result := imageStatusResult(t, sender)
	if !provider.checkLatest {
		t.Error("expected check_latest to be forwarded")
	}
	if result.RequestId != "req-1" || result.ApplicationId != "app-1" || result.ErrorMessage != "" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(result.Images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(result.Images))
	}
	img := result.Images[0]
	if img.Service != "web" || img.CurrentDigest != "sha256:old" || img.CurrentVersion != "1.27.0" ||
		img.LatestDigest != "sha256:new" || !img.UpdateAvailable {
		t.Fatalf("unexpected image: %+v", img)
	}
}

func TestExecuteImageStatus_ReportsError(t *testing.T) {
	sender := &stubSender{sent: make(chan *messages.ClientMessage, 1)}

	executeImageStatus(t.Context(), sender, &stubImageStatusProvider{err: errors.New("compose file not found")}, &messages.ImageStatusRequest{RequestId: "req-1"})

	if result := imageStatusResult(t, sender); result.ErrorMessage != "compose file not found" {
		t.Fatalf("unexpected error message %q", result.ErrorMessage)
	}
}

func TestExecuteImageStatus_UnsupportedProvider(t *testing.T) {
	sender := &stubSender{sent: make(chan *messages.ClientMessage, 1)}

	executeImageStatus(t.Context(), sender, nil, &messages.ImageStatusRequest{RequestId: "req-1"})

	if result := imageStatusResult(t, sender); result.RequestId != "req-1" || result.ErrorMessage == "" {
		t.Fatalf("expected an error result, got %+v", result)
	}
}
