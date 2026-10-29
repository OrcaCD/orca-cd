package healthhistory

import (
	"math"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
)

func seedChange(t *testing.T, appID string, status models.HealthStatus, at time.Time, services ...models.ServiceHealth) {
	t.Helper()
	change := models.ApplicationHealthChange{ApplicationId: appID, Status: status, ChangedAt: at, Services: services}
	if status == models.UnknownHealth {
		reason := models.HealthChangeReasonAgentOffline
		change.Reason = &reason
	}
	if err := db.DB.Create(&change).Error; err != nil {
		t.Fatalf("seed health change: %v", err)
	}
}

func seedDeployment(t *testing.T, appID string, createdAt, completedAt time.Time, commit string) {
	t.Helper()
	seedEvent(t, models.ApplicationEvent{
		ApplicationId: appID,
		Status:        models.ApplicationEventSucceeded,
		CommitHash:    &commit,
		CompletedAt:   &completedAt,
	}, createdAt)
}

func seedEvent(t *testing.T, event models.ApplicationEvent, createdAt time.Time) {
	t.Helper()
	event.Type = models.ApplicationEventDeployment
	event.Source = models.ApplicationEventSourceManual
	if err := db.DB.Create(&event).Error; err != nil {
		t.Fatalf("seed deployment: %v", err)
	}
	// BeforeCreate always stamps the current time.
	if err := db.DB.Model(&event).UpdateColumn("created_at", createdAt).Error; err != nil {
		t.Fatalf("backdate deployment: %v", err)
	}
}

func getStats(t *testing.T, appID string, r Range, now time.Time) Stats {
	t.Helper()
	stats, err := GetStats(t.Context(), appID, r, now)
	if err != nil {
		t.Fatalf("GetStats() error: %v", err)
	}
	return stats
}

func TestGetStats_WithoutHistory(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t, "agent-1")

	stats := getStats(t, app.Id, Range7d, time.Now())
	if stats.Current != nil || stats.Availability != nil {
		t.Errorf("expected no current state and availability, got %+v / %v", stats.Current, stats.Availability)
	}
	if len(stats.Buckets) != 56 || stats.BucketSeconds != int64((3*time.Hour).Seconds()) {
		t.Fatalf("expected 56 buckets of 3h, got %d of %ds", len(stats.Buckets), stats.BucketSeconds)
	}
	for _, bucket := range stats.Buckets {
		if bucket.Status != nil {
			t.Fatalf("expected only untracked buckets, got %q", *bucket.Status)
		}
	}
	if stats.NoDataSeconds != 0 {
		t.Errorf("untracked time must not count as no data, got %ds", stats.NoDataSeconds)
	}
	if len(stats.Incidents) != 0 || stats.DataGapCount != 0 {
		t.Errorf("the time before the first change must not be a data gap, got %+v", stats.Incidents)
	}
}

func TestGetStats_Day(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t, "agent-1")
	now := time.Now().UTC().Truncate(time.Second)
	at := func(d time.Duration) time.Time { return now.Add(-d) }

	seedChange(t, app.Id, models.Healthy, at(48*time.Hour))
	seedChange(t, app.Id, models.Unhealthy, at(10*time.Hour), models.ServiceHealth{Name: "web", Status: models.Unhealthy})
	seedChange(t, app.Id, models.Healthy, at(9*time.Hour+30*time.Minute))
	seedChange(t, app.Id, models.UnknownHealth, at(5*time.Hour))
	seedChange(t, app.Id, models.Healthy, at(4*time.Hour))
	seedDeployment(t, app.Id, at(2*time.Hour+5*time.Minute), at(2*time.Hour), "a1b2c3d")
	seedChange(t, app.Id, models.Stopped, at(2*time.Hour-time.Minute))
	seedChange(t, app.Id, models.Healthy, at(2*time.Hour-11*time.Minute))
	seedChange(t, app.Id, models.Degraded, at(30*time.Minute), models.ServiceHealth{Name: "worker", Status: models.Stopped})

	stats := getStats(t, app.Id, Range24h, now)

	if stats.Current == nil || stats.Current.Status != models.Degraded || !stats.Current.Since.Equal(at(30*time.Minute)) {
		t.Errorf("expected current degraded since 30m ago, got %+v", stats.Current)
	}
	wantDowntime := int64((30*time.Minute + 10*time.Minute + 30*time.Minute).Seconds())
	if stats.DowntimeSeconds != wantDowntime {
		t.Errorf("expected downtime %ds, got %ds", wantDowntime, stats.DowntimeSeconds)
	}
	if stats.NoDataSeconds != int64(time.Hour.Seconds()) {
		t.Errorf("expected 1h without data, got %ds", stats.NoDataSeconds)
	}
	wantAvailability := float64(23*3600-wantDowntime) / float64(23*3600) * 100
	if stats.Availability == nil || math.Abs(*stats.Availability-wantAvailability) > 0.001 {
		t.Errorf("expected availability %.3f, got %v", wantAvailability, stats.Availability)
	}
	if stats.IncidentCount != 3 || stats.DataGapCount != 1 {
		t.Errorf("expected 3 incidents and 1 data gap, got %d and %d", stats.IncidentCount, stats.DataGapCount)
	}
	if stats.LongestIncidentSeconds != int64((30 * time.Minute).Seconds()) {
		t.Errorf("expected longest incident of 30m, got %ds", stats.LongestIncidentSeconds)
	}

	if len(stats.Incidents) != 4 {
		t.Fatalf("expected 4 incidents, got %d", len(stats.Incidents))
	}
	ongoing, stopped, gap, unhealthy := stats.Incidents[0], stats.Incidents[1], stats.Incidents[2], stats.Incidents[3]
	if ongoing.Status != models.Degraded || ongoing.EndedAt != nil || len(ongoing.Services) != 1 || ongoing.Services[0].Name != "worker" {
		t.Errorf("expected ongoing degraded incident with service worker, got %+v", ongoing)
	}
	if stopped.Status != models.Stopped || stopped.Trigger == nil || stopped.Trigger.Type != models.ApplicationEventDeployment ||
		stopped.Trigger.CommitHash == nil || *stopped.Trigger.CommitHash != "a1b2c3d" {
		t.Errorf("expected stopped incident after deploy a1b2c3d, got %+v", stopped)
	}
	if gap.Status != models.UnknownHealth || gap.Reason == nil || *gap.Reason != models.HealthChangeReasonAgentOffline ||
		gap.DurationSeconds != int64(time.Hour.Seconds()) {
		t.Errorf("expected 1h agent offline gap, got %+v", gap)
	}
	if unhealthy.Status != models.Unhealthy || unhealthy.Trigger != nil || unhealthy.EndedAt == nil {
		t.Errorf("expected ended unhealthy incident without trigger, got %+v", unhealthy)
	}

	if len(stats.Buckets) != 48 || stats.BucketSeconds != int64((30*time.Minute).Seconds()) {
		t.Fatalf("expected 48 buckets of 30m, got %d of %ds", len(stats.Buckets), stats.BucketSeconds)
	}
	// Bucket i covers (24-i/2)h..(23.5-i/2)h ago. The unhealthy incident starts
	// exactly at the end of bucket 27, so that bucket stays healthy.
	wantStatus := map[int]models.HealthStatus{
		28: models.Unhealthy,
		38: models.UnknownHealth,
		39: models.UnknownHealth,
		44: models.Stopped,
		47: models.Degraded,
	}
	for i, bucket := range stats.Buckets {
		want, ok := wantStatus[i]
		if !ok {
			want = models.Healthy
		}
		if bucket.Status == nil || *bucket.Status != want {
			t.Errorf("bucket %d: expected %q, got %v", i, want, bucket.Status)
		}
	}
}

func TestGetStats_MergesConsecutiveFailures(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t, "agent-1")
	now := time.Now().UTC().Truncate(time.Second)

	seedChange(t, app.Id, models.Degraded, now.Add(-3*time.Hour), models.ServiceHealth{Name: "web", Status: models.Unhealthy})
	seedChange(t, app.Id, models.Stopped, now.Add(-2*time.Hour), models.ServiceHealth{Name: "db", Status: models.Stopped}, models.ServiceHealth{Name: "web", Status: models.Stopped})
	seedChange(t, app.Id, models.Healthy, now.Add(-time.Hour))

	stats := getStats(t, app.Id, Range24h, now)
	if stats.IncidentCount != 1 || len(stats.Incidents) != 1 {
		t.Fatalf("expected one merged incident, got %+v", stats.Incidents)
	}
	incident := stats.Incidents[0]
	if incident.Status != models.Stopped || incident.DurationSeconds != int64((2*time.Hour).Seconds()) {
		t.Errorf("expected a 2h stopped incident, got %+v", incident)
	}
	want := []models.ServiceHealth{{Name: "db", Status: models.Stopped}, {Name: "web", Status: models.Stopped}}
	if len(incident.Services) != 2 || incident.Services[0] != want[0] || incident.Services[1] != want[1] {
		t.Errorf("expected services %v, got %v", want, incident.Services)
	}
	if !incident.StartedAt.Equal(now.Add(-3 * time.Hour)) {
		t.Errorf("expected the incident to start with the first failure, got %v", incident.StartedAt)
	}
}

func TestGetStats_IncidentBeforeRangeKeepsRealStart(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t, "agent-1")
	now := time.Now().UTC().Truncate(time.Second)

	seedChange(t, app.Id, models.Unhealthy, now.Add(-26*time.Hour))
	seedChange(t, app.Id, models.Healthy, now.Add(-23*time.Hour))

	stats := getStats(t, app.Id, Range24h, now)
	if stats.DowntimeSeconds != int64(time.Hour.Seconds()) {
		t.Errorf("expected only the downtime inside the range to count, got %ds", stats.DowntimeSeconds)
	}
	if len(stats.Incidents) != 1 || !stats.Incidents[0].StartedAt.Equal(now.Add(-26*time.Hour)) ||
		stats.Incidents[0].DurationSeconds != int64((3*time.Hour).Seconds()) {
		t.Errorf("expected the incident to keep its real start and duration, got %+v", stats.Incidents)
	}
}

func TestGetStats_NewlyTrackedApplication(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t, "agent-1")
	now := time.Now().UTC().Truncate(time.Second)

	seedChange(t, app.Id, models.Healthy, now.Add(-2*time.Minute))

	stats := getStats(t, app.Id, Range24h, now)
	if stats.Availability == nil || *stats.Availability != 100 {
		t.Errorf("expected 100%% availability, got %v", stats.Availability)
	}
	if stats.NoDataSeconds != 0 || stats.DataGapCount != 0 {
		t.Errorf("time before the first change must not count as no data, got %ds and %d gaps", stats.NoDataSeconds, stats.DataGapCount)
	}
	last := stats.Buckets[len(stats.Buckets)-1]
	if last.Status == nil || *last.Status != models.Healthy {
		t.Errorf("expected the current bucket to be healthy, got %v", last.Status)
	}
	for _, bucket := range stats.Buckets[:len(stats.Buckets)-1] {
		if bucket.Status != nil {
			t.Fatalf("expected earlier buckets to be untracked, got %q", *bucket.Status)
		}
	}
}

func TestGetStats_TriggerAttribution(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	at := func(d time.Duration) time.Time { return now.Add(-d) }
	commit := func(hash string) *string { return &hash }

	tests := []struct {
		name   string
		events []models.ApplicationEvent
		want   *string
	}{
		{
			name: "deployment without changes is ignored",
			events: []models.ApplicationEvent{
				{Status: models.ApplicationEventSucceeded, CommitHash: commit("changed"), CreatedAt: at(3 * time.Hour), CompletedAt: new(at(3 * time.Hour))},
				{Status: models.ApplicationEventNoChange, CommitHash: commit("unchanged"), CreatedAt: at(2*time.Hour + 2*time.Minute), CompletedAt: new(at(2*time.Hour + 2*time.Minute))},
				{Status: models.ApplicationEventSucceeded, CommitHash: commit("deployed"), CreatedAt: at(2*time.Hour + 5*time.Minute), CompletedAt: new(at(2*time.Hour + 3*time.Minute))},
			},
			want: commit("deployed"),
		},
		{
			name: "running deployment",
			events: []models.ApplicationEvent{
				{Status: models.ApplicationEventRunning, CommitHash: commit("running"), CreatedAt: at(2*time.Hour + 10*time.Minute)},
			},
			want: commit("running"),
		},
		{
			name: "stuck running deployment",
			events: []models.ApplicationEvent{
				{Status: models.ApplicationEventRunning, CommitHash: commit("stuck"), CreatedAt: at(5 * time.Hour)},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupTestDB(t)
			app := seedApp(t, "agent-1")
			seedChange(t, app.Id, models.Healthy, at(6*time.Hour))
			seedChange(t, app.Id, models.Unhealthy, at(2*time.Hour))
			for _, event := range tt.events {
				event.ApplicationId = app.Id
				seedEvent(t, event, event.CreatedAt)
			}

			stats := getStats(t, app.Id, Range24h, now)
			if len(stats.Incidents) != 1 {
				t.Fatalf("expected 1 incident, got %d", len(stats.Incidents))
			}
			trigger := stats.Incidents[0].Trigger
			switch {
			case tt.want == nil && trigger != nil:
				t.Errorf("expected no trigger, got %+v", trigger)
			case tt.want != nil && (trigger == nil || trigger.CommitHash == nil || *trigger.CommitHash != *tt.want):
				t.Errorf("expected trigger %q, got %+v", *tt.want, trigger)
			}
		})
	}
}

func TestGetStats_BoundsLoadedChanges(t *testing.T) {
	setupTestDB(t)
	app := seedApp(t, "agent-1")
	now := time.Now().UTC().Truncate(time.Second)
	seedChange(t, app.Id, models.Healthy, now.Add(-48*time.Hour))

	// The latest maxChanges changes alternate every second within the last
	// maxChanges seconds, the change before them lies earlier in the range.
	changes := make([]models.ApplicationHealthChange, 0, maxChanges+1)
	changes = append(changes, models.ApplicationHealthChange{ApplicationId: app.Id, Status: models.Unhealthy, ChangedAt: now.Add(-20 * time.Hour)})
	for i := range maxChanges {
		status := models.Healthy
		if i%2 == 1 {
			status = models.Stopped
		}
		changes = append(changes, models.ApplicationHealthChange{ApplicationId: app.Id, Status: status, ChangedAt: now.Add(-time.Duration(maxChanges-i) * time.Second)})
	}
	if err := db.DB.CreateInBatches(changes, 500).Error; err != nil {
		t.Fatalf("seed health changes: %v", err)
	}

	stats := getStats(t, app.Id, Range24h, now)
	if !stats.Truncated {
		t.Error("expected the statistics to be truncated")
	}
	if stats.IncidentCount != maxChanges/2 {
		t.Errorf("expected %d incidents from the loaded changes, got %d", maxChanges/2, stats.IncidentCount)
	}
	if stats.HealthySeconds+stats.DowntimeSeconds != maxChanges {
		t.Errorf("expected only the loaded changes to be observed, got %ds", stats.HealthySeconds+stats.DowntimeSeconds)
	}
	if got := stats.Buckets[0].Status; got != nil {
		t.Errorf("expected the time before the loaded changes to be untracked, got %q", *got)
	}
}
