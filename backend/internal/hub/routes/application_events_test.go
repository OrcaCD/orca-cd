package routes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

func setupApplicationEventsRouteTest(t *testing.T) models.Application {
	t.Helper()
	setupTestDBWithApplications(t)
	if err := db.DB.AutoMigrate(&models.ApplicationEvent{}); err != nil {
		t.Fatalf("migrate application events: %v", err)
	}
	repo := seedTestRepository(t, "https://github.com/owner/event-history")
	agent := seedTestAgent(t, "event-agent")
	return seedTestApplication(t, repo.Id, agent.Id, "Event App")
}

func seedApplicationEventAt(t *testing.T, appID, id string, createdAt time.Time) {
	t.Helper()
	event := models.ApplicationEvent{
		Id:            id,
		ApplicationId: appID,
		Type:          models.ApplicationEventCommitSync,
		Source:        models.ApplicationEventSourceRepositoryWebhook,
		Status:        models.ApplicationEventNoChange,
	}
	if err := gorm.G[models.ApplicationEvent](db.DB).Create(t.Context(), &event); err != nil {
		t.Fatalf("seed application event: %v", err)
	}
	if _, err := gorm.G[models.ApplicationEvent](db.DB).
		Where("id = ?", event.Id).
		Update(t.Context(), "created_at", createdAt); err != nil {
		t.Fatalf("set application event timestamp: %v", err)
	}
}

func invokeApplicationEventsHandler(t *testing.T, appID, query string) *httptest.ResponseRecorder {
	t.Helper()
	c, w := makeAuthContext(t, "user-1")
	c.Params = ginParams("id", appID)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/applications/"+appID+"/events"+query, nil)
	ListApplicationEventsHandler(c)
	return w
}

func ginParams(key, value string) []gin.Param {
	return []gin.Param{{Key: key, Value: value}}
}

func TestListApplicationEventsHandlerOrdersAndPaginates(t *testing.T) {
	app := setupApplicationEventsRouteTest(t)
	base := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	for i := range 3 {
		seedApplicationEventAt(t, app.Id, fmt.Sprintf("event-%d", i), base.Add(time.Duration(i)*time.Minute))
	}

	w := invokeApplicationEventsHandler(t, app.Id, "?limit=1&offset=1")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var response struct {
		Items   []applicationEventResponse `json:"items"`
		HasMore bool                       `json:"hasMore"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Items) != 1 || response.Items[0].Id != "event-1" {
		t.Fatalf("items=%+v", response.Items)
	}
	if !response.HasMore {
		t.Fatal("hasMore=false, want true")
	}
	if _, err := time.Parse(time.RFC3339Nano, response.Items[0].CreatedAt); err != nil {
		t.Fatalf("createdAt is not RFC3339Nano: %v", err)
	}
	if strings.Contains(w.Body.String(), "requestId") {
		t.Fatalf("internal requestId leaked: %s", w.Body.String())
	}
}

func TestListApplicationEventsHandlerRejectsInvalidPagination(t *testing.T) {
	app := setupApplicationEventsRouteTest(t)
	for _, query := range []string{"?limit=0", "?limit=101", "?limit=abc", "?offset=-1", "?offset=abc"} {
		t.Run(query, func(t *testing.T) {
			w := invokeApplicationEventsHandler(t, app.Id, query)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestListApplicationEventsHandlerReturnsNotFound(t *testing.T) {
	setupApplicationEventsRouteTest(t)
	w := invokeApplicationEventsHandler(t, "missing", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestListApplicationEventsHandlerFiltersBySearch(t *testing.T) {
	app := setupApplicationEventsRouteTest(t)
	events := []models.ApplicationEvent{
		{
			Id:            "event-deploy",
			ApplicationId: app.Id,
			Type:          models.ApplicationEventDeployment,
			Source:        models.ApplicationEventSourceManual,
			Status:        models.ApplicationEventSucceeded,
			ActorName:     new("Alice"),
			CommitHash:    new("abc1234def"),
			CommitMessage: new("Bump nginx to 1.27"),
		},
		{
			Id:            "event-sync",
			ApplicationId: app.Id,
			Type:          models.ApplicationEventCommitSync,
			Source:        models.ApplicationEventSourceRepositoryWebhook,
			Status:        models.ApplicationEventFailed,
			ErrorMessage:  new("compose file not found"),
		},
	}
	for i := range events {
		if err := gorm.G[models.ApplicationEvent](db.DB).Create(t.Context(), &events[i]); err != nil {
			t.Fatalf("seed application event: %v", err)
		}
	}

	tests := []struct {
		search string
		want   []string
	}{
		{search: "alice", want: []string{"event-deploy"}},
		{search: "abc123", want: []string{"event-deploy"}},
		{search: "commit_sync", want: []string{"event-sync"}},
		{search: "repository_webhook", want: []string{"event-sync"}},
		{search: "not found", want: []string{"event-sync"}},
		{search: "  ", want: []string{"event-sync", "event-deploy"}},
	}
	for _, tt := range tests {
		t.Run(tt.search, func(t *testing.T) {
			w := invokeApplicationEventsHandler(t, app.Id, "?search="+url.QueryEscape(tt.search))
			if w.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			var response struct {
				Items []applicationEventResponse `json:"items"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			got := make([]string, 0, len(response.Items))
			for _, item := range response.Items {
				got = append(got, item.Id)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("ids=%v, want %v", got, tt.want)
			}
		})
	}
}

func captureApplicationEventsLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var output bytes.Buffer
	previous := applicationEventsLog
	applicationEventsLog = zerolog.New(&output)
	t.Cleanup(func() { applicationEventsLog = previous })
	return &output
}

func TestListApplicationEventsHandlerLogsApplicationLookupError(t *testing.T) {
	app := setupApplicationEventsRouteTest(t)
	logs := captureApplicationEventsLog(t)
	closeDBForErrorPath(t)
	w := invokeApplicationEventsHandler(t, app.Id, "")
	if w.Code != http.StatusInternalServerError || w.Body.String() != "{\"error\":\"internal server error\"}" {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(logs.String(), "failed to load application for event history") ||
		!strings.Contains(logs.String(), app.Id) {
		t.Fatalf("expected application lookup failure to be logged, got %q", logs.String())
	}
}

func TestListApplicationEventsHandlerLogsEventQueryError(t *testing.T) {
	app := setupApplicationEventsRouteTest(t)
	logs := captureApplicationEventsLog(t)
	if err := db.DB.Exec("DROP TABLE application_events").Error; err != nil {
		t.Fatalf("drop application events table: %v", err)
	}

	w := invokeApplicationEventsHandler(t, app.Id, "")
	if w.Code != http.StatusInternalServerError || w.Body.String() != "{\"error\":\"internal server error\"}" {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(logs.String(), "failed to list application events") ||
		!strings.Contains(logs.String(), app.Id) {
		t.Fatalf("expected event query failure to be logged, got %q", logs.String())
	}
}

func TestEventActorSnapshotsClaims(t *testing.T) {
	c, _ := makeAuthContext(t, "actor-1")
	userID, userName := eventActor(c)
	if userID == nil || *userID != "actor-1" || userName == nil || *userName != "Test User" {
		t.Fatalf("eventActor() = %v, %v", userID, userName)
	}
}
