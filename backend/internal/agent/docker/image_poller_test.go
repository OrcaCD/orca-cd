package docker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/rs/zerolog"
)

type stubMessageSender struct {
	mu   sync.Mutex
	msgs []*messages.ClientMessage
	err  error
}

func (s *stubMessageSender) SendMessage(msg *messages.ClientMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = append(s.msgs, msg)
	return s.err
}

func (s *stubMessageSender) received() []*messages.ClientMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*messages.ClientMessage(nil), s.msgs...)
}

func newTestPoller(t *testing.T, sender MessageSender) *ImagePoller {
	t.Helper()
	c := newTestClient(t)
	return NewImagePoller(c, sender, zerolog.Nop())
}

func applyOne(p *ImagePoller, appID, appName string, settings PollSettings) {
	p.ApplySettings([]AppPollConfig{{AppID: appID, AppName: appName, Settings: settings}})
}

func TestNewImagePoller_EmptyApps(t *testing.T) {
	p := newTestPoller(t, nil)
	if len(p.apps) != 0 {
		t.Error("expected empty apps map")
	}
}

func TestImagePoller_SettingsFor_Unknown(t *testing.T) {
	p := newTestPoller(t, nil)
	if got := p.SettingsFor("unknown-id"); got != nil {
		t.Errorf("expected nil for unknown app, got %+v", got)
	}
}

func TestImagePoller_ApplySettings_Disabled(t *testing.T) {
	p := newTestPoller(t, nil)
	applyOne(p, "app-1", "myapp", PollSettings{Enabled: false})

	p.mu.Lock()
	_, exists := p.apps["app-1"]
	p.mu.Unlock()
	if exists {
		t.Error("expected disabled app to be removed from map")
	}
}

func TestImagePoller_ApplySettings_MinimumInterval(t *testing.T) {
	p := newTestPoller(t, nil)
	applyOne(p, "app-1", "myapp", PollSettings{Enabled: true, IntervalSeconds: 5})

	got := p.SettingsFor("app-1")
	if got == nil {
		t.Fatal("expected settings to exist")
	}
	if got.IntervalSeconds != minPollIntervalSeconds {
		t.Errorf("expected interval clamped to %d, got %d", minPollIntervalSeconds, got.IntervalSeconds)
	}
}

func TestImagePoller_SettingsFor_ReturnsCorrectValues(t *testing.T) {
	p := newTestPoller(t, nil)
	settings := PollSettings{Enabled: true, IntervalSeconds: 120, DeleteOldImages: true}
	applyOne(p, "app-2", "billing", settings)

	got := p.SettingsFor("app-2")
	if got == nil {
		t.Fatal("expected settings to exist")
	}
	if got.IntervalSeconds != 120 {
		t.Errorf("expected interval 120, got %d", got.IntervalSeconds)
	}
	if !got.DeleteOldImages {
		t.Error("expected DeleteOldImages to be true")
	}
}

func TestImagePoller_StopAll_IsIdempotent(t *testing.T) {
	p := newTestPoller(t, nil)
	applyOne(p, "app-1", "myapp", PollSettings{Enabled: true, IntervalSeconds: 60})
	p.StopAll()
	p.StopAll() // second call must not panic
}

func TestImagePoller_StopAll_ClearsMap(t *testing.T) {
	p := newTestPoller(t, nil)
	applyOne(p, "app-1", "a", PollSettings{Enabled: true, IntervalSeconds: 60})
	applyOne(p, "app-2", "b", PollSettings{Enabled: true, IntervalSeconds: 60})
	p.StopAll()

	p.mu.Lock()
	count := len(p.apps)
	p.mu.Unlock()
	if count != 0 {
		t.Errorf("expected empty map after StopAll, got %d entries", count)
	}
}

func TestImagePoller_ApplySettings_StopsOldTicker(t *testing.T) {
	p := newTestPoller(t, nil)
	applyOne(p, "app-1", "myapp", PollSettings{Enabled: true, IntervalSeconds: 60})

	p.mu.Lock()
	firstStop := p.apps["app-1"].stop
	p.mu.Unlock()

	// Re-configure — should close old stop channel and create a new one.
	applyOne(p, "app-1", "myapp", PollSettings{Enabled: true, IntervalSeconds: 90})

	p.mu.Lock()
	secondStop := p.apps["app-1"].stop
	p.mu.Unlock()

	if firstStop == secondStop {
		t.Error("expected new stop channel after reconfigure")
	}

	select {
	case <-firstStop:
		// closed correctly
	default:
		t.Error("expected old stop channel to be closed after reconfigure")
	}

	p.StopAll()
}

func TestImagePoller_ApplySettings_RemovesStaleApp(t *testing.T) {
	p := newTestPoller(t, nil)

	// Start with two apps.
	p.ApplySettings([]AppPollConfig{
		{AppID: "app-1", AppName: "a", Settings: PollSettings{Enabled: true, IntervalSeconds: 60}},
		{AppID: "app-2", AppName: "b", Settings: PollSettings{Enabled: true, IntervalSeconds: 60}},
	})

	p.mu.Lock()
	removedStop := p.apps["app-2"].stop
	p.mu.Unlock()

	// New snapshot omits app-2 — its ticker must be stopped.
	p.ApplySettings([]AppPollConfig{
		{AppID: "app-1", AppName: "a", Settings: PollSettings{Enabled: true, IntervalSeconds: 60}},
	})

	select {
	case <-removedStop:
		// stopped correctly
	default:
		t.Error("expected stop channel of removed app to be closed")
	}

	if got := p.SettingsFor("app-2"); got != nil {
		t.Error("expected app-2 to be absent after removal from snapshot")
	}
	if got := p.SettingsFor("app-1"); got == nil {
		t.Error("expected app-1 to still be present")
	}

	p.StopAll()
}

func TestImagePoller_RunOnce_SendsResultOnUpdate(t *testing.T) {
	origCheck := checkAndPullImages
	t.Cleanup(func() { checkAndPullImages = origCheck })

	checkAndPullImages = func(_ context.Context, _ *Client, _, _ string, _ bool) (bool, error) {
		return true, nil // images updated
	}

	sender := &stubMessageSender{}
	p := newTestPoller(t, sender)
	applyOne(p, "app-1", "myapp", PollSettings{Enabled: true, IntervalSeconds: 60})
	defer p.StopAll()

	p.runOnce("app-1", "myapp", "req-1")

	msgs := sender.received()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	result := msgs[0].GetPullImagesResult()
	if result == nil {
		t.Fatal("expected PullImagesResult payload")
	}
	if !result.Success {
		t.Error("expected success=true")
	}
	if !result.ImagesUpdated {
		t.Error("expected images_updated=true")
	}
	if result.RequestId != "req-1" {
		t.Errorf("expected request_id %q, got %q", "req-1", result.RequestId)
	}
}

func TestImagePoller_RunOnce_SendsResultOnError(t *testing.T) {
	origCheck := checkAndPullImages
	t.Cleanup(func() { checkAndPullImages = origCheck })

	checkAndPullImages = func(_ context.Context, _ *Client, _, _ string, _ bool) (bool, error) {
		return false, errors.New("registry unreachable")
	}

	sender := &stubMessageSender{}
	p := newTestPoller(t, sender)
	applyOne(p, "app-1", "myapp", PollSettings{Enabled: true, IntervalSeconds: 60})
	defer p.StopAll()

	p.runOnce("app-1", "myapp", "")

	msgs := sender.received()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	result := msgs[0].GetPullImagesResult()
	if result == nil {
		t.Fatal("expected PullImagesResult payload")
	}
	if result.Success {
		t.Error("expected success=false on error")
	}
	if result.ErrorMessage == "" {
		t.Error("expected non-empty error message")
	}
}

func TestImagePoller_RunOnce_SilentWhenNothingChanged(t *testing.T) {
	origCheck := checkAndPullImages
	t.Cleanup(func() { checkAndPullImages = origCheck })

	checkAndPullImages = func(_ context.Context, _ *Client, _, _ string, _ bool) (bool, error) {
		return false, nil // nothing changed
	}

	sender := &stubMessageSender{}
	p := newTestPoller(t, sender)
	applyOne(p, "app-1", "myapp", PollSettings{Enabled: true, IntervalSeconds: 60})
	defer p.StopAll()

	p.runOnce("app-1", "myapp", "")

	if msgs := sender.received(); len(msgs) != 0 {
		t.Errorf("expected no message when nothing changed, got %d", len(msgs))
	}
}

func TestImagePoller_RunOnce_SendsResultOnExplicitRequestEvenWhenNothingChanged(t *testing.T) {
	origCheck := checkAndPullImages
	t.Cleanup(func() { checkAndPullImages = origCheck })

	checkAndPullImages = func(_ context.Context, _ *Client, _, _ string, _ bool) (bool, error) {
		return false, nil // nothing changed, e.g. a duplicate webhook delivery
	}

	sender := &stubMessageSender{}
	p := newTestPoller(t, sender)
	applyOne(p, "app-1", "myapp", PollSettings{Enabled: true, IntervalSeconds: 60})
	defer p.StopAll()

	p.runOnce("app-1", "myapp", "req-1")

	msgs := sender.received()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	result := msgs[0].GetPullImagesResult()
	if result == nil {
		t.Fatal("expected PullImagesResult payload")
	}
	if !result.Success {
		t.Error("expected success=true")
	}
	if result.ImagesUpdated {
		t.Error("expected images_updated=false")
	}
	if result.RequestId != "req-1" {
		t.Errorf("expected request_id %q, got %q", "req-1", result.RequestId)
	}
}

func TestImagePoller_RunOnce_SendErrorLogged(t *testing.T) {
	origCheck := checkAndPullImages
	t.Cleanup(func() { checkAndPullImages = origCheck })

	checkAndPullImages = func(_ context.Context, _ *Client, _, _ string, _ bool) (bool, error) {
		return true, nil // trigger a send
	}

	sender := &stubMessageSender{err: errors.New("send failed")}
	p := newTestPoller(t, sender)
	applyOne(p, "app-1", "myapp", PollSettings{Enabled: true, IntervalSeconds: 60})
	defer p.StopAll()

	// Must not panic when sender returns an error; the error is logged.
	p.runOnce("app-1", "myapp", "req-1")
}

type noopSender struct{}

func (noopSender) SendMessage(_ *messages.ClientMessage) error { return nil }

func TestImagePoller_RunOnce_NoopSenderIsNoOp(t *testing.T) {
	origCheck := checkAndPullImages
	t.Cleanup(func() { checkAndPullImages = origCheck })

	checkAndPullImages = func(_ context.Context, _ *Client, _, _ string, _ bool) (bool, error) {
		return true, nil
	}

	c := newTestClient(t)
	p := NewImagePoller(c, noopSender{}, zerolog.Nop())
	applyOne(p, "app-1", "myapp", PollSettings{Enabled: true, IntervalSeconds: 60})
	defer p.StopAll()

	p.runOnce("app-1", "myapp", "") // must not panic
}

func TestImagePoller_TriggerNow(t *testing.T) {
	origCheck := checkAndPullImages
	t.Cleanup(func() { checkAndPullImages = origCheck })

	called := make(chan struct{}, 1)
	checkAndPullImages = func(_ context.Context, _ *Client, _, appName string, _ bool) (bool, error) {
		if appName == "billing" {
			called <- struct{}{}
		}
		return false, nil
	}

	p := newTestPoller(t, noopSender{})
	p.TriggerNow("app-1", "billing", "req-99")

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for TriggerNow to call checkAndPullImages")
	}
}

func TestImagePoller_TriggerNow_SerializesSameApplication(t *testing.T) {
	origCheck := checkAndPullImages
	t.Cleanup(func() { checkAndPullImages = origCheck })

	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseAll := func() {
		releaseOnce.Do(func() {
			close(release)
		})
	}
	t.Cleanup(releaseAll)

	checkAndPullImages = func(_ context.Context, _ *Client, _, _ string, _ bool) (bool, error) {
		started <- struct{}{}
		<-release
		return false, nil
	}

	p := newTestPoller(t, noopSender{})
	p.TriggerNow("app-1", "billing", "req-1")

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first pull to start")
	}

	p.TriggerNow("app-1", "billing", "req-2")

	select {
	case <-started:
		t.Fatal("second pull started while first pull was still running")
	case <-time.After(100 * time.Millisecond):
	}

	releaseAll()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second pull to run after first completed")
	}
}
