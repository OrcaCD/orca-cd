package docker

import (
	"context"
	"errors"
	"fmt"
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
	return NewImagePoller(c, sender, zerolog.Nop(), 0)
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

func TestImagePoller_ApplySettings_KeepsUnchangedTicker(t *testing.T) {
	p := newTestPoller(t, nil)
	applyOne(p, "app-1", "myapp", PollSettings{Enabled: true, IntervalSeconds: 30})

	p.mu.Lock()
	firstStop := p.apps["app-1"].stop
	p.mu.Unlock()

	// Normalized to the same minimum interval as before.
	applyOne(p, "app-1", "myapp", PollSettings{Enabled: true, IntervalSeconds: 45})

	p.mu.Lock()
	secondStop := p.apps["app-1"].stop
	p.mu.Unlock()
	if firstStop != secondStop {
		t.Error("expected the unchanged ticker to keep running")
	}
	select {
	case <-firstStop:
		t.Error("expected the unchanged ticker not to be stopped")
	default:
	}

	applyOne(p, "app-1", "renamed", PollSettings{Enabled: true, IntervalSeconds: 60})
	p.mu.Lock()
	renamedStop := p.apps["app-1"].stop
	p.mu.Unlock()
	if renamedStop == firstStop {
		t.Error("expected a renamed application to get a new ticker")
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

	origListImages := listServiceImages
	t.Cleanup(func() { listServiceImages = origListImages })

	checkAndPullImages = func(_ context.Context, _ *Client, _, _ string, _ bool, _ ReleaseAgePolicy) (bool, []*messages.ImageUpdate, error) {
		return true, nil, nil // images updated
	}
	listServiceImages = func(_ context.Context, _ *Client, appID string) ([]*messages.ServiceImage, error) {
		return []*messages.ServiceImage{{Service: "app", ImageId: "sha256:new-" + appID}}, nil
	}

	sender := &stubMessageSender{}
	p := newTestPoller(t, sender)
	applyOne(p, "app-1", "myapp", PollSettings{Enabled: true, IntervalSeconds: 60})
	defer p.StopAll()

	p.runOnce("app-1", "myapp", "req-1", false, ReleaseAgePolicy{})

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
	if len(result.Images) != 1 || result.Images[0].ImageId != "sha256:new-app-1" {
		t.Errorf("expected the updated images in the result, got %v", result.Images)
	}
}

func TestImagePoller_RunOnce_SendsResultOnError(t *testing.T) {
	origCheck := checkAndPullImages
	t.Cleanup(func() { checkAndPullImages = origCheck })

	checkAndPullImages = func(_ context.Context, _ *Client, _, _ string, _ bool, _ ReleaseAgePolicy) (bool, []*messages.ImageUpdate, error) {
		return false, nil, errors.New("registry unreachable")
	}

	sender := &stubMessageSender{}
	p := newTestPoller(t, sender)
	applyOne(p, "app-1", "myapp", PollSettings{Enabled: true, IntervalSeconds: 60})
	defer p.StopAll()

	p.runOnce("app-1", "myapp", "", false, ReleaseAgePolicy{})

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

	checkAndPullImages = func(_ context.Context, _ *Client, _, _ string, _ bool, _ ReleaseAgePolicy) (bool, []*messages.ImageUpdate, error) {
		return false, nil, nil // nothing changed
	}

	sender := &stubMessageSender{}
	p := newTestPoller(t, sender)
	applyOne(p, "app-1", "myapp", PollSettings{Enabled: true, IntervalSeconds: 60})
	defer p.StopAll()

	p.runOnce("app-1", "myapp", "", false, ReleaseAgePolicy{})

	if msgs := sender.received(); len(msgs) != 0 {
		t.Errorf("expected no message when nothing changed, got %d", len(msgs))
	}
}

func TestImagePoller_RunOnce_ReportsRecoveryOnceAfterFailure(t *testing.T) {
	origCheck := checkAndPullImages
	t.Cleanup(func() { checkAndPullImages = origCheck })

	var checkErr error
	checkAndPullImages = func(_ context.Context, _ *Client, _, _ string, _ bool, _ ReleaseAgePolicy) (bool, []*messages.ImageUpdate, error) {
		return false, nil, checkErr
	}

	sender := &stubMessageSender{}
	p := newTestPoller(t, sender)
	applyOne(p, "app-1", "myapp", PollSettings{Enabled: true, IntervalSeconds: 60})
	defer p.StopAll()

	checkErr = errors.New("unauthorized")
	p.runOnce("app-1", "myapp", "", false, ReleaseAgePolicy{})
	p.runOnce("app-1", "myapp", "", false, ReleaseAgePolicy{})
	checkErr = nil
	p.runOnce("app-1", "myapp", "", false, ReleaseAgePolicy{})
	p.runOnce("app-1", "myapp", "", false, ReleaseAgePolicy{})

	msgs := sender.received()
	if len(msgs) != 3 {
		t.Fatalf("expected 2 failures and 1 recovery, got %d messages", len(msgs))
	}
	for i, wantSuccess := range []bool{false, false, true} {
		result := msgs[i].GetPullImagesResult()
		if result == nil {
			t.Fatalf("message %d: expected PullImagesResult payload", i)
		}
		if result.Success != wantSuccess {
			t.Errorf("message %d: success = %v, want %v", i, result.Success, wantSuccess)
		}
		if result.ImagesUpdated {
			t.Errorf("message %d: expected images_updated=false", i)
		}
	}
}

func TestImagePoller_RunOnce_RecoveryStateIsPerApplication(t *testing.T) {
	origCheck := checkAndPullImages
	t.Cleanup(func() { checkAndPullImages = origCheck })

	checkAndPullImages = func(_ context.Context, _ *Client, appID, _ string, _ bool, _ ReleaseAgePolicy) (bool, []*messages.ImageUpdate, error) {
		if appID == "app-failing" {
			return false, nil, errors.New("unauthorized")
		}
		return false, nil, nil
	}

	sender := &stubMessageSender{}
	p := newTestPoller(t, sender)

	p.runOnce("app-failing", "a", "", false, ReleaseAgePolicy{})
	p.runOnce("app-healthy", "b", "", false, ReleaseAgePolicy{})

	msgs := sender.received()
	if len(msgs) != 1 {
		t.Fatalf("expected only the failure to be reported, got %d messages", len(msgs))
	}
	if got := msgs[0].GetPullImagesResult().ApplicationId; got != "app-failing" {
		t.Errorf("expected the failing application to be reported, got %q", got)
	}
}

func TestImagePoller_RunOnce_SendsResultOnExplicitRequestEvenWhenNothingChanged(t *testing.T) {
	origCheck := checkAndPullImages
	t.Cleanup(func() { checkAndPullImages = origCheck })

	checkAndPullImages = func(_ context.Context, _ *Client, _, _ string, _ bool, _ ReleaseAgePolicy) (bool, []*messages.ImageUpdate, error) {
		return false, nil, nil // nothing changed, e.g. a duplicate webhook delivery
	}

	sender := &stubMessageSender{}
	p := newTestPoller(t, sender)
	applyOne(p, "app-1", "myapp", PollSettings{Enabled: true, IntervalSeconds: 60})
	defer p.StopAll()

	p.runOnce("app-1", "myapp", "req-1", false, ReleaseAgePolicy{})

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

	checkAndPullImages = func(_ context.Context, _ *Client, _, _ string, _ bool, _ ReleaseAgePolicy) (bool, []*messages.ImageUpdate, error) {
		return true, nil, nil // trigger a send
	}

	sender := &stubMessageSender{err: errors.New("send failed")}
	p := newTestPoller(t, sender)
	applyOne(p, "app-1", "myapp", PollSettings{Enabled: true, IntervalSeconds: 60})
	defer p.StopAll()

	// Must not panic when sender returns an error; the error is logged.
	p.runOnce("app-1", "myapp", "req-1", false, ReleaseAgePolicy{})
}

type noopSender struct{}

func (noopSender) SendMessage(_ *messages.ClientMessage) error { return nil }

func TestImagePoller_RunOnce_NoopSenderIsNoOp(t *testing.T) {
	origCheck := checkAndPullImages
	t.Cleanup(func() { checkAndPullImages = origCheck })

	checkAndPullImages = func(_ context.Context, _ *Client, _, _ string, _ bool, _ ReleaseAgePolicy) (bool, []*messages.ImageUpdate, error) {
		return true, nil, nil
	}

	c := newTestClient(t)
	p := NewImagePoller(c, noopSender{}, zerolog.Nop(), 0)
	applyOne(p, "app-1", "myapp", PollSettings{Enabled: true, IntervalSeconds: 60})
	defer p.StopAll()

	p.runOnce("app-1", "myapp", "", false, ReleaseAgePolicy{}) // must not panic
}

func TestImagePoller_IntervalOverride(t *testing.T) {
	origCheck := checkAndPullImages
	t.Cleanup(func() { checkAndPullImages = origCheck })

	called := make(chan struct{}, 1)
	checkAndPullImages = func(_ context.Context, _ *Client, _, _ string, _ bool, _ ReleaseAgePolicy) (bool, []*messages.ImageUpdate, error) {
		select {
		case called <- struct{}{}:
		default:
		}
		return false, nil, nil
	}

	p := NewImagePoller(newTestClient(t), noopSender{}, zerolog.Nop(), 10*time.Millisecond)
	applyOne(p, "app-1", "myapp", PollSettings{Enabled: true, IntervalSeconds: 60})
	defer p.StopAll()

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a tick with the overridden interval")
	}
	// The configured interval is kept, only the ticker uses the override
	if got := p.SettingsFor("app-1"); got == nil || got.IntervalSeconds != 60 {
		t.Errorf("expected stored interval 60, got %+v", got)
	}
}

func TestImagePoller_TriggerNow(t *testing.T) {
	origCheck := checkAndPullImages
	t.Cleanup(func() { checkAndPullImages = origCheck })

	called := make(chan struct{}, 1)
	checkAndPullImages = func(_ context.Context, _ *Client, _, appName string, _ bool, _ ReleaseAgePolicy) (bool, []*messages.ImageUpdate, error) {
		if appName == "billing" {
			called <- struct{}{}
		}
		return false, nil, nil
	}

	p := newTestPoller(t, noopSender{})
	p.TriggerNow("app-1", "billing", "req-99", false, ReleaseAgePolicy{})

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for TriggerNow to call checkAndPullImages")
	}
}

func TestImagePoller_TriggerNow_PassesDeleteOldWithoutPollSettings(t *testing.T) {
	origCheck := checkAndPullImages
	t.Cleanup(func() { checkAndPullImages = origCheck })

	got := make(chan bool, 1)
	checkAndPullImages = func(_ context.Context, _ *Client, _, _ string, deleteOld bool, _ ReleaseAgePolicy) (bool, []*messages.ImageUpdate, error) {
		got <- deleteOld
		return false, nil, nil
	}

	// No ApplySettings: polling is disabled for the app, but an explicit
	// trigger (webhook, manual, GitHub Actions) must still honor cleanup.
	p := newTestPoller(t, noopSender{})
	p.TriggerNow("app-1", "billing", "req-1", true, ReleaseAgePolicy{})

	select {
	case deleteOld := <-got:
		if !deleteOld {
			t.Error("expected deleteOld=true to be passed to checkAndPullImages")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for TriggerNow to call checkAndPullImages")
	}
}

func TestImagePoller_RunTicker_UsesPollSettingsDeleteOld(t *testing.T) {
	origCheck := checkAndPullImages
	t.Cleanup(func() { checkAndPullImages = origCheck })

	got := make(chan bool, 1)
	checkAndPullImages = func(_ context.Context, _ *Client, _, _ string, deleteOld bool, _ ReleaseAgePolicy) (bool, []*messages.ImageUpdate, error) {
		select {
		case got <- deleteOld:
		default:
		}
		return false, nil, nil
	}

	p := newTestPoller(t, noopSender{})
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		p.runTicker("app-1", "myapp", PollSettings{Enabled: true, IntervalSeconds: 1, DeleteOldImages: true}, stop)
		close(done)
	}()

	select {
	case deleteOld := <-got:
		if !deleteOld {
			t.Error("expected periodic ticks to pass deleteOld from the poll settings")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a tick")
	}

	close(stop)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ticker did not stop")
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

	checkAndPullImages = func(_ context.Context, _ *Client, _, _ string, _ bool, _ ReleaseAgePolicy) (bool, []*messages.ImageUpdate, error) {
		started <- struct{}{}
		<-release
		return false, nil, nil
	}

	p := newTestPoller(t, noopSender{})
	p.TriggerNow("app-1", "billing", "req-1", false, ReleaseAgePolicy{})

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first pull to start")
	}

	p.TriggerNow("app-1", "billing", "req-2", false, ReleaseAgePolicy{})

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

func TestImagePoller_RunOnce_ReportsFailedRedeploy(t *testing.T) {
	origCheck := checkAndPullImages
	t.Cleanup(func() { checkAndPullImages = origCheck })

	for _, tt := range []struct {
		err  error
		want bool
	}{
		{fmt.Errorf("%w: container exited", ErrRedeployFailed), true},
		{errors.New("registry unreachable"), false},
	} {
		checkAndPullImages = func(_ context.Context, _ *Client, _, _ string, _ bool, _ ReleaseAgePolicy) (bool, []*messages.ImageUpdate, error) {
			return false, nil, tt.err
		}
		sender := &stubMessageSender{}
		p := newTestPoller(t, sender)

		p.runOnce("app-1", "myapp", "req-1", false, ReleaseAgePolicy{})

		msgs := sender.received()
		if len(msgs) != 1 {
			t.Fatalf("expected 1 message, got %d", len(msgs))
		}
		if got := msgs[0].GetPullImagesResult().RedeployFailed; got != tt.want {
			t.Errorf("%v: redeploy_failed = %v, want %v", tt.err, got, tt.want)
		}
	}
}

func TestImagePoller_RunOnce_ReportsHeldBackImagesWhenTheyChange(t *testing.T) {
	origCheck := checkAndPullImages
	t.Cleanup(func() { checkAndPullImages = origCheck })

	var images []*messages.ImageUpdate
	checkAndPullImages = func(_ context.Context, _ *Client, _, _ string, _ bool, _ ReleaseAgePolicy) (bool, []*messages.ImageUpdate, error) {
		return false, images, nil
	}
	waiting := func(reason string) []*messages.ImageUpdate {
		return []*messages.ImageUpdate{{
			Service: "cache",
			Image:   "redis:7",
			Status:  messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_WAITING,
			Reason:  reason,
		}}
	}

	sender := &stubMessageSender{}
	p := newTestPoller(t, sender)
	applyOne(p, "app-1", "myapp", PollSettings{Enabled: true, IntervalSeconds: 60})
	defer p.StopAll()

	images = waiting("waiting until tomorrow")
	p.runOnce("app-1", "myapp", "", false, ReleaseAgePolicy{})
	p.runOnce("app-1", "myapp", "", false, ReleaseAgePolicy{})
	images = waiting("waiting until next week")
	p.runOnce("app-1", "myapp", "", false, ReleaseAgePolicy{})
	images = []*messages.ImageUpdate{{Service: "cache", Image: "redis:7", Status: messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_UP_TO_DATE}}
	p.runOnce("app-1", "myapp", "", false, ReleaseAgePolicy{})

	var results []*messages.PullImagesResult
	reports := 0
	for _, msg := range sender.received() {
		if result := msg.GetPullImagesResult(); result != nil {
			results = append(results, result)
		}
		if msg.GetImageUpdatesReport() != nil {
			reports++
		}
	}
	if reports != 4 {
		t.Errorf("expected an image updates report for every run, got %d", reports)
	}
	if len(results) != 2 {
		t.Fatalf("expected a result for each change of the held back images, got %d", len(results))
	}
	for i, wantReason := range []string{"waiting until tomorrow", "waiting until next week"} {
		result := results[i]
		if !result.Success || len(result.HeldBackImages) != 1 || result.HeldBackImages[0].Reason != wantReason {
			t.Errorf("result %d: unexpected %v", i, result)
		}
	}
}
