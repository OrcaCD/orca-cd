package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/healthhistory"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
)

func setupApplicationHealthRouteTest(t *testing.T) models.Application {
	t.Helper()
	setupTestDBWithApplications(t)
	if err := db.DB.AutoMigrate(&models.ApplicationHealthChange{}); err != nil {
		t.Fatalf("migrate application health changes: %v", err)
	}
	repo := seedTestRepository(t, "https://github.com/owner/health-stats")
	agent := seedTestAgent(t, "health-agent")
	return seedTestApplication(t, repo.Id, agent.Id, "Health App")
}

func invokeApplicationHealthHandler(t *testing.T, appID, query string) *httptest.ResponseRecorder {
	t.Helper()
	c, w := makeAuthContext(t, "user-1")
	c.Params = ginParams("id", appID)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/applications/"+appID+"/health"+query, nil)
	GetApplicationHealthHandler(c)
	return w
}

func TestGetApplicationHealthHandler(t *testing.T) {
	app := setupApplicationHealthRouteTest(t)
	if err := db.DB.Create(&models.ApplicationHealthChange{
		ApplicationId: app.Id,
		Status:        models.Healthy,
		ChangedAt:     time.Now().Add(-2 * time.Hour),
	}).Error; err != nil {
		t.Fatalf("seed health change: %v", err)
	}

	w := invokeApplicationHealthHandler(t, app.Id, "?range=7d")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var stats healthhistory.Stats
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if stats.Range != healthhistory.Range7d || len(stats.Buckets) != 56 {
		t.Errorf("expected 7d range with 56 buckets, got %q with %d", stats.Range, len(stats.Buckets))
	}
	if stats.Current == nil || stats.Current.Status != models.Healthy {
		t.Errorf("expected current healthy status, got %+v", stats.Current)
	}
	if stats.Availability == nil || *stats.Availability != 100 {
		t.Errorf("expected 100%% availability, got %v", stats.Availability)
	}
}

func TestGetApplicationHealthHandlerDefaultsToDay(t *testing.T) {
	app := setupApplicationHealthRouteTest(t)

	w := invokeApplicationHealthHandler(t, app.Id, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var stats healthhistory.Stats
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if stats.Range != healthhistory.Range24h || stats.Availability != nil {
		t.Errorf("expected 24h range without availability, got %q / %v", stats.Range, stats.Availability)
	}
}

func TestGetApplicationHealthHandlerRejectsInvalidRange(t *testing.T) {
	app := setupApplicationHealthRouteTest(t)

	if w := invokeApplicationHealthHandler(t, app.Id, "?range=90d"); w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestGetApplicationHealthHandlerUnknownApplication(t *testing.T) {
	setupApplicationHealthRouteTest(t)

	if w := invokeApplicationHealthHandler(t, "missing", "?range=24h"); w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}
