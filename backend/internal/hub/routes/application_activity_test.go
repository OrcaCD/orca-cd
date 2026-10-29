package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"gorm.io/gorm"
)

func seedActivityEvent(t *testing.T, appID string, status models.ApplicationEventStatus, createdAt time.Time) models.ApplicationEvent {
	t.Helper()
	commit := "abcdef1234567"
	event := models.ApplicationEvent{
		ApplicationId: appID,
		Type:          models.ApplicationEventDeployment,
		Source:        models.ApplicationEventSourceManual,
		Status:        status,
		CommitHash:    &commit,
	}
	if err := gorm.G[models.ApplicationEvent](db.DB).Create(t.Context(), &event); err != nil {
		t.Fatalf("seed application event: %v", err)
	}
	if _, err := gorm.G[models.ApplicationEvent](db.DB).
		Where("id = ?", event.Id).
		Update(t.Context(), "created_at", createdAt); err != nil {
		t.Fatalf("set application event timestamp: %v", err)
	}
	return event
}

func invokeApplicationActivityHandler(t *testing.T, query string) (*httptest.ResponseRecorder, applicationActivityResponse) {
	t.Helper()
	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/activity"+query, nil)
	ApplicationActivityHandler(c)

	var response applicationActivityResponse
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
	return w, response
}

func TestApplicationActivityHandlerEmpty(t *testing.T) {
	setupApplicationEventsRouteTest(t)

	w, response := invokeApplicationActivityHandler(t, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if len(response.Days) != applicationActivityDays {
		t.Fatalf("expected %d days, got %d", applicationActivityDays, len(response.Days))
	}
	today := time.Now().UTC().Format(time.DateOnly)
	if last := response.Days[len(response.Days)-1].Date; last != today {
		t.Fatalf("expected last day %s, got %s", today, last)
	}
	for _, day := range response.Days {
		if day.Succeeded != 0 || day.Failed != 0 {
			t.Fatalf("expected empty day, got %+v", day)
		}
	}
	if len(response.RecentDeployments) != 0 {
		t.Fatalf("expected no recent deployments, got %+v", response.RecentDeployments)
	}
	if value := rawJSONField(t, w.Body.String(), "recentDeployments"); value != "[]" {
		t.Fatalf("expected recentDeployments to be an empty array, got %s", value)
	}
}

func rawJSONField(t *testing.T, body, key string) string {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatalf("decode raw response: %v", err)
	}
	return string(raw[key])
}

func TestApplicationActivityHandlerCountsCompletedDeploymentsPerDay(t *testing.T) {
	app := setupApplicationEventsRouteTest(t)
	now := time.Now()

	seedActivityEvent(t, app.Id, models.ApplicationEventSucceeded, now.Add(-time.Minute))
	seedActivityEvent(t, app.Id, models.ApplicationEventFailed, now.Add(-2*time.Minute))
	seedActivityEvent(t, app.Id, models.ApplicationEventNoChange, now.Add(-3*time.Minute))
	seedActivityEvent(t, app.Id, models.ApplicationEventRunning, now.Add(-4*time.Minute))
	seedActivityEvent(t, app.Id, models.ApplicationEventSucceeded, now.AddDate(0, 0, -2))
	seedActivityEvent(t, app.Id, models.ApplicationEventSucceeded, now.AddDate(0, 0, -10))

	w, response := invokeApplicationActivityHandler(t, "?utcOffset=0")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	counts := map[string]applicationActivityDay{}
	totalSucceeded, totalFailed := 0, 0
	for _, day := range response.Days {
		counts[day.Date] = day
		totalSucceeded += day.Succeeded
		totalFailed += day.Failed
	}
	if totalSucceeded != 2 || totalFailed != 1 {
		t.Fatalf("expected 2 succeeded and 1 failed, got %d and %d: %+v", totalSucceeded, totalFailed, response.Days)
	}
	twoDaysAgo := counts[now.UTC().AddDate(0, 0, -2).Format(time.DateOnly)]
	if twoDaysAgo.Succeeded != 1 {
		t.Fatalf("expected one success two days ago, got %+v", response.Days)
	}
}

func TestApplicationActivityHandlerBucketsByUTCOffset(t *testing.T) {
	app := setupApplicationEventsRouteTest(t)
	// 23:30 UTC yesterday is already today at UTC+01:00.
	todayUTC := time.Now().UTC().Truncate(24 * time.Hour)
	seedActivityEvent(t, app.Id, models.ApplicationEventSucceeded, todayUTC.Add(-30*time.Minute))

	// Compare by date instead of position, since "today" at UTC+01:00 is
	// already the next day between 23:00 and 24:00 UTC.
	_, utcResponse := invokeApplicationActivityHandler(t, "?utcOffset=0")
	yesterday := todayUTC.AddDate(0, 0, -1).Format(time.DateOnly)
	if day := findActivityDay(t, utcResponse.Days, yesterday); day.Succeeded != 1 {
		t.Fatalf("expected event on %s in UTC, got %+v", yesterday, utcResponse.Days)
	}

	_, shiftedResponse := invokeApplicationActivityHandler(t, "?utcOffset=60")
	today := todayUTC.Format(time.DateOnly)
	if day := findActivityDay(t, shiftedResponse.Days, today); day.Succeeded != 1 {
		t.Fatalf("expected event on %s at UTC+01:00, got %+v", today, shiftedResponse.Days)
	}
}

func findActivityDay(t *testing.T, days []applicationActivityDay, date string) applicationActivityDay {
	t.Helper()
	for _, day := range days {
		if day.Date == date {
			return day
		}
	}
	t.Fatalf("expected %s in activity days, got %+v", date, days)
	return applicationActivityDay{}
}

func TestApplicationActivityHandlerReturnsRecentDeployments(t *testing.T) {
	app := setupApplicationEventsRouteTest(t)
	now := time.Now()
	older := seedActivityEvent(t, app.Id, models.ApplicationEventSucceeded, now.Add(-time.Hour))
	latest := seedActivityEvent(t, app.Id, models.ApplicationEventFailed, now.Add(-time.Minute))
	seedActivityEvent(t, app.Id, models.ApplicationEventNoChange, now)

	w, response := invokeApplicationActivityHandler(t, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if len(response.RecentDeployments) != 2 {
		t.Fatalf("expected 2 recent deployments without the no-change event, got %+v", response.RecentDeployments)
	}
	if response.RecentDeployments[1].Id != older.Id {
		t.Fatalf("expected older event %s last, got %+v", older.Id, response.RecentDeployments[1])
	}
	deployment := response.RecentDeployments[0]
	if deployment.Id != latest.Id || deployment.Status != string(models.ApplicationEventFailed) {
		t.Fatalf("expected latest failed event %s first, got %+v", latest.Id, deployment)
	}
	if deployment.ApplicationId != app.Id || deployment.ApplicationName != "Event App" || deployment.ApplicationIcon != "box" {
		t.Fatalf("unexpected application fields: %+v", deployment)
	}
	if deployment.CommitHash == nil || *deployment.CommitHash != "abcdef1234567" {
		t.Fatalf("expected commit hash, got %+v", deployment.CommitHash)
	}
}

func TestApplicationActivityHandlerLimitsRecentDeployments(t *testing.T) {
	app := setupApplicationEventsRouteTest(t)
	now := time.Now()
	for i := range applicationActivityRecentDeployments + 2 {
		seedActivityEvent(t, app.Id, models.ApplicationEventSucceeded, now.Add(-time.Duration(i)*time.Minute))
	}

	_, response := invokeApplicationActivityHandler(t, "")
	if len(response.RecentDeployments) != applicationActivityRecentDeployments {
		t.Fatalf("expected %d recent deployments, got %d", applicationActivityRecentDeployments, len(response.RecentDeployments))
	}
}

func TestApplicationActivityHandlerRejectsInvalidOffset(t *testing.T) {
	setupApplicationEventsRouteTest(t)
	for _, query := range []string{"?utcOffset=abc", "?utcOffset=841", "?utcOffset=-841"} {
		w, _ := invokeApplicationActivityHandler(t, query)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("query %s: expected 400, got %d", query, w.Code)
		}
	}
}

func TestApplicationActivityHandlerDBError(t *testing.T) {
	setupApplicationEventsRouteTest(t)
	closeDBForErrorPath(t)

	w, _ := invokeApplicationActivityHandler(t, "")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestApplicationActivityHandlerIncludesDeploymentStatus(t *testing.T) {
	app := setupApplicationEventsRouteTest(t)
	event := seedActivityEvent(t, app.Id, models.ApplicationEventRunning, time.Now())
	deployment := models.ApplicationDeployment{
		ApplicationId: app.Id,
		Status:        models.DeploymentVerifying,
		Images:        []models.DeployedImage{},
	}
	if err := gorm.G[models.ApplicationDeployment](db.DB).Create(t.Context(), &deployment); err != nil {
		t.Fatalf("seed deployment: %v", err)
	}
	if _, err := gorm.G[models.ApplicationEvent](db.DB).Where("id = ?", event.Id).
		Update(t.Context(), "deployment_id", deployment.Id); err != nil {
		t.Fatalf("link deployment: %v", err)
	}

	w, response := invokeApplicationActivityHandler(t, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if len(response.RecentDeployments) != 1 {
		t.Fatalf("expected 1 recent deployment, got %+v", response.RecentDeployments)
	}
	got := response.RecentDeployments[0]
	if got.DeploymentStatus == nil || *got.DeploymentStatus != string(models.DeploymentVerifying) {
		t.Errorf("deployment status = %v, want verifying", got.DeploymentStatus)
	}
}
