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
	event := models.ApplicationEvent{
		ApplicationId: appID,
		Type:          models.ApplicationEventDeployment,
		Source:        models.ApplicationEventSourceManual,
		Status:        models.ApplicationEventSucceeded,
		CommitHash:    &commit,
		CompletedAt:   &completedAt,
	}
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

	if len(stats.Buckets) != 24 {
		t.Fatalf("expected 24 hourly buckets, got %d", len(stats.Buckets))
	}
	// Bucket i covers (24-i)h..(23-i)h ago. The unhealthy incident starts
	// exactly at the end of bucket 13, so that bucket stays healthy.
	wantStatus := map[int]models.HealthStatus{
		14: models.Unhealthy,
		19: models.UnknownHealth,
		22: models.Stopped,
		23: models.Degraded,
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
