package applications

import (
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	hubws "github.com/OrcaCD/orca-cd/internal/hub/websocket"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// setupDebouncer installs a debouncer with test timings as the default. It must be
// called after seedConnectedApp so pending pulls are stopped before the cleanups
// that tear down the hub and database run.
func setupDebouncer(t *testing.T, window, maxDelay time.Duration) *ImagePullDebouncer {
	t.Helper()
	log := zerolog.Nop()
	debouncer := NewImagePullDebouncer(&log)
	debouncer.window = window
	debouncer.maxDelay = maxDelay
	DefaultImagePullDebouncer = debouncer
	t.Cleanup(func() {
		debouncer.Stop()
		DefaultImagePullDebouncer = nil
	})
	return debouncer
}

// seedConnectedApp seeds an application whose agent has an open hub connection,
// so triggered pulls land on the returned client's send channel.
func seedConnectedApp(t *testing.T) (*models.Application, *hubws.Client) {
	t.Helper()
	log := zerolog.Nop()
	hub := hubws.NewHub(&log)
	hubws.DefaultHub = hub
	t.Cleanup(func() { hubws.DefaultHub = nil })

	agent := seedAgent(t)
	app := seedImagePullApp(t, agent.Id)
	client, err := hub.Register(agent.Id, nil)
	if err != nil {
		t.Fatalf("failed to register agent: %v", err)
	}
	return app, client
}

func awaitPullRequest(t *testing.T, client *hubws.Client) *messages.PullImagesRequest {
	t.Helper()
	select {
	case msg := <-client.Send:
		req := msg.GetPullImagesRequest()
		if req == nil {
			t.Fatalf("expected PullImagesRequest, got %T", msg.Payload)
		}
		return req
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a PullImagesRequest")
		return nil
	}
}

func expectNoPullRequest(t *testing.T, client *hubws.Client, within time.Duration) {
	t.Helper()
	select {
	case msg := <-client.Send:
		t.Fatalf("expected no further request, got %T", msg.Payload)
	case <-time.After(within):
	}
}

func countImageUpdateEvents(t *testing.T, appID string) int {
	t.Helper()
	events, err := gorm.G[models.ApplicationEvent](db.DB).
		Where("application_id = ? AND type = ?", appID, models.ApplicationEventImageUpdate).
		Find(t.Context())
	if err != nil {
		t.Fatalf("failed to load events: %v", err)
	}
	return len(events)
}

func TestScheduleImagePull_CoalescesBurstIntoSinglePull(t *testing.T) {
	setupTestDB(t)
	app, client := seedConnectedApp(t)
	setupDebouncer(t, 40*time.Millisecond, time.Second)

	// A single registry push arrives as several webhook deliveries.
	for range 3 {
		ScheduleImagePull(app, models.ApplicationEventSourceImageWebhook)
		time.Sleep(5 * time.Millisecond)
	}

	req := awaitPullRequest(t, client)
	if req.ApplicationId != app.Id {
		t.Errorf("application_id: got %q, want %q", req.ApplicationId, app.Id)
	}
	expectNoPullRequest(t, client, 200*time.Millisecond)

	if got := countImageUpdateEvents(t, app.Id); got != 1 {
		t.Errorf("expected 1 image_update event for the burst, got %d", got)
	}
}

func TestScheduleImagePull_SeparateBurstsPullAgain(t *testing.T) {
	setupTestDB(t)
	app, client := seedConnectedApp(t)
	setupDebouncer(t, 30*time.Millisecond, time.Second)

	ScheduleImagePull(app, models.ApplicationEventSourceImageWebhook)
	awaitPullRequest(t, client)

	ScheduleImagePull(app, models.ApplicationEventSourceImageWebhook)
	awaitPullRequest(t, client)

	if got := countImageUpdateEvents(t, app.Id); got != 2 {
		t.Errorf("expected 2 image_update events for 2 bursts, got %d", got)
	}
}

func TestScheduleImagePull_MaxDelayCapsPostponement(t *testing.T) {
	setupTestDB(t)
	app, client := seedConnectedApp(t)
	// A quiet window this long never settles while triggers keep arriving; only
	// the max delay can release the pull.
	setupDebouncer(t, 5*time.Second, 40*time.Millisecond)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 20 {
			ScheduleImagePull(app, models.ApplicationEventSourceImageWebhook)
			time.Sleep(10 * time.Millisecond)
		}
	}()

	awaitPullRequest(t, client)
	<-done
}

func TestScheduleImagePull_ImmediateTriggerSupersedesQueuedPull(t *testing.T) {
	setupTestDB(t)
	app, client := seedConnectedApp(t)
	debouncer := setupDebouncer(t, time.Hour, time.Hour)

	ScheduleImagePull(app, models.ApplicationEventSourceImageWebhook)
	if !TriggerImagePull(app, models.ApplicationEventSourceGitHubActions) {
		t.Fatal("expected TriggerImagePull to return true for connected agent")
	}

	req := awaitPullRequest(t, client)
	if req.ApplicationId != app.Id {
		t.Errorf("application_id: got %q, want %q", req.ApplicationId, app.Id)
	}

	debouncer.mu.Lock()
	pending := len(debouncer.pending)
	debouncer.mu.Unlock()
	if pending != 0 {
		t.Errorf("expected the queued pull to be dropped, got %d pending", pending)
	}
}

func TestImagePullDebouncer_Stop_DropsQueuedPull(t *testing.T) {
	setupTestDB(t)
	app, client := seedConnectedApp(t)
	debouncer := setupDebouncer(t, 30*time.Millisecond, time.Second)

	ScheduleImagePull(app, models.ApplicationEventSourceImageWebhook)
	debouncer.Stop()

	expectNoPullRequest(t, client, 200*time.Millisecond)
	if got := countImageUpdateEvents(t, app.Id); got != 0 {
		t.Errorf("expected no image_update event after Stop, got %d", got)
	}

	// Triggers after Stop are ignored as well.
	ScheduleImagePull(app, models.ApplicationEventSourceImageWebhook)
	expectNoPullRequest(t, client, 100*time.Millisecond)
}

func TestImagePullDebouncer_DeletedApplication_NoPull(t *testing.T) {
	setupTestDB(t)
	app, client := seedConnectedApp(t)
	setupDebouncer(t, 30*time.Millisecond, time.Second)

	ScheduleImagePull(app, models.ApplicationEventSourceImageWebhook)
	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).Delete(t.Context()); err != nil {
		t.Fatalf("failed to delete application: %v", err)
	}

	expectNoPullRequest(t, client, 300*time.Millisecond)
}

func TestScheduleImagePull_NoDebouncer_TriggersImmediately(t *testing.T) {
	setupTestDB(t)
	app, client := seedConnectedApp(t)
	DefaultImagePullDebouncer = nil

	ScheduleImagePull(app, models.ApplicationEventSourceImageWebhook)

	select {
	case msg := <-client.Send:
		if msg.GetPullImagesRequest() == nil {
			t.Errorf("expected PullImagesRequest, got %T", msg.Payload)
		}
	default:
		t.Error("expected an immediate PullImagesRequest without a debouncer")
	}
}
