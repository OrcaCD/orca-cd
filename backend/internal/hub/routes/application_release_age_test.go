package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func invokeReleaseAgeHandler(t *testing.T, handler gin.HandlerFunc, method, path, body string, params gin.Params) *httptest.ResponseRecorder {
	t.Helper()
	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(method, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = params
	handler(c)
	return w
}

func invokeReleaseAgeSettingsHandler(t *testing.T, appID, body string) *httptest.ResponseRecorder {
	t.Helper()
	return invokeReleaseAgeHandler(t, UpdateReleaseAgeSettingsHandler, http.MethodPut,
		"/api/v1/applications/"+appID+"/release-age", body, gin.Params{{Key: "id", Value: appID}})
}

func decodeApplication(t *testing.T, w *httptest.ResponseRecorder) applicationResponse {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response applicationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return response
}

func TestUpdateReleaseAgeSettingsHandler(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-release-age")
	agent := seedTestAgent(t, "agent-release-age")
	app := seedTestApplication(t, repo.Id, agent.Id, "Release Age App")

	response := decodeApplication(t, invokeReleaseAgeSettingsHandler(t, app.Id,
		`{"minimumSeconds":259200,"allowedRegistries":[" Registry.local:5000 ","registry.local:5000"]}`))
	want := []string{"registry.local:5000"}
	if response.ReleaseAge.MinimumSeconds == nil || *response.ReleaseAge.MinimumSeconds != 259200 ||
		!slices.Equal(response.ReleaseAge.AllowedRegistries, want) {
		t.Errorf("release age settings = %+v", response.ReleaseAge)
	}

	// null goes back to the global default
	response = decodeApplication(t, invokeReleaseAgeSettingsHandler(t, app.Id, `{"minimumSeconds":null,"allowedRegistries":[]}`))
	if response.ReleaseAge.MinimumSeconds != nil {
		t.Errorf("expected the global default, got %d", *response.ReleaseAge.MinimumSeconds)
	}
	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(t.Context())
	if err != nil {
		t.Fatalf("load application: %v", err)
	}
	if updated.MinimumReleaseAgeSeconds != nil || len(updated.ReleaseAgeAllowedRegistries) != 0 {
		t.Errorf("unexpected stored settings: %v %v", updated.MinimumReleaseAgeSeconds, updated.ReleaseAgeAllowedRegistries)
	}
}

func TestUpdateReleaseAgeSettingsHandler_Rejects(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-release-age-invalid")
	agent := seedTestAgent(t, "agent-release-age-invalid")
	app := seedTestApplication(t, repo.Id, agent.Id, "Release Age App")

	for _, tt := range []struct {
		id, body string
		want     int
	}{
		{app.Id, `{"minimumSeconds":-1,"allowedRegistries":[]}`, http.StatusBadRequest},
		{app.Id, `{"minimumSeconds":31536001,"allowedRegistries":[]}`, http.StatusBadRequest},
		{app.Id, `{"minimumSeconds":0,"allowedRegistries":["registry.local/library"]}`, http.StatusBadRequest},
		{app.Id, `{"minimumSeconds":0,"allowedRegistries":[""]}`, http.StatusBadRequest},
		{app.Id, `not json`, http.StatusBadRequest},
		{"missing", `{"minimumSeconds":0,"allowedRegistries":[]}`, http.StatusNotFound},
	} {
		if w := invokeReleaseAgeSettingsHandler(t, tt.id, tt.body); w.Code != tt.want {
			t.Errorf("%s %s: expected %d, got %d: %s", tt.id, tt.body, tt.want, w.Code, w.Body.String())
		}
	}
}

func TestUpdateServiceReleaseAgeHandler(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-release-age-service")
	agent := seedTestAgent(t, "agent-release-age-service")
	app := seedTestApplication(t, repo.Id, agent.Id, "Release Age Service App")
	setService := func(service, body string) *httptest.ResponseRecorder {
		return invokeReleaseAgeHandler(t, UpdateServiceReleaseAgeHandler, http.MethodPut,
			"/api/v1/applications/"+app.Id+"/release-age/services/"+service, body,
			gin.Params{{Key: "id", Value: app.Id}, {Key: "service", Value: service}})
	}

	decodeApplication(t, setService("db", `{"minimumSeconds":604800}`))
	response := decodeApplication(t, setService("web", `{"minimumSeconds":0}`))
	if got := response.ReleaseAge.ServiceOverrides; len(got) != 2 || got["db"] != 604800 || got["web"] != 0 {
		t.Errorf("service overrides = %v", got)
	}

	response = decodeApplication(t, setService("db", `{"minimumSeconds":null}`))
	if got := response.ReleaseAge.ServiceOverrides; len(got) != 1 || got["web"] != 0 {
		t.Errorf("expected the db override to be removed, got %v", got)
	}

	if w := setService("db", `{"minimumSeconds":-5}`); w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for a negative age, got %d", w.Code)
	}
}

func TestAdminUpdateReleaseAgeSettingsHandler(t *testing.T) {
	setupTestDBWithApplications(t)

	w := invokeReleaseAgeHandler(t, AdminUpdateReleaseAgeSettingsHandler, http.MethodPut,
		"/api/v1/admin/settings/release-age", `{"minimumSeconds":86400,"allowedRegistries":["Registry.Local"]}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w = invokeReleaseAgeHandler(t, GetReleaseAgeSettingsHandler, http.MethodGet, "/api/v1/settings/release-age", "", nil)
	var settings globalReleaseAgeSettings
	if err := json.Unmarshal(w.Body.Bytes(), &settings); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if settings.MinimumSeconds != 86400 || !slices.Equal(settings.AllowedRegistries, []string{"registry.local"}) {
		t.Errorf("global settings = %+v", settings)
	}

	w = invokeReleaseAgeHandler(t, AdminUpdateReleaseAgeSettingsHandler, http.MethodPut,
		"/api/v1/admin/settings/release-age", `{"minimumSeconds":-1,"allowedRegistries":[]}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for a negative age, got %d", w.Code)
	}
}

func TestListApplicationImageUpdatesHandler(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-image-updates")
	agent := seedTestAgent(t, "agent-image-updates")
	app := seedTestApplication(t, repo.Id, agent.Id, "Image Updates App")
	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).
		Select("ReleaseAgeServiceOverrides").
		Updates(t.Context(), models.Application{ReleaseAgeServiceOverrides: map[string]int64{"db": 604800}}); err != nil {
		t.Fatalf("update application: %v", err)
	}

	checkedAt := time.Now().UTC().Truncate(time.Second)
	eligibleAt := checkedAt.Add(48 * time.Hour)
	rows := []models.ApplicationImageUpdate{
		{ApplicationId: app.Id, Service: "web", Image: "ghcr.io/org/web:1", Status: models.ImageUpdateUpToDate, MinimumAgeSeconds: 0, MinimumAgeFromLabel: true, CheckedAt: checkedAt},
		{ApplicationId: app.Id, Service: "db", Image: "postgres:17", Status: models.ImageUpdateWaiting, NewVersion: "17.2", EligibleAt: &eligibleAt, MinimumAgeSeconds: 604800, CheckedAt: checkedAt},
		{ApplicationId: app.Id, Service: "cache", Image: "redis:7", Status: models.ImageUpdateUpToDate, CheckedAt: checkedAt},
	}
	if err := gorm.G[models.ApplicationImageUpdate](db.DB).CreateInBatches(t.Context(), &rows, 10); err != nil {
		t.Fatalf("seed image updates: %v", err)
	}

	w := invokeReleaseAgeHandler(t, ListApplicationImageUpdatesHandler, http.MethodGet,
		"/api/v1/applications/"+app.Id+"/image-updates", "", gin.Params{{Key: "id", Value: app.Id}})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response imageUpdatesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.CheckedAt == nil || len(response.Images) != 3 {
		t.Fatalf("unexpected response %+v", response)
	}
	sources := map[string]string{}
	for _, image := range response.Images {
		sources[image.Service] = image.AgeSource
	}
	if sources["web"] != "label" || sources["db"] != "service" || sources["cache"] != "global" {
		t.Errorf("age sources = %v", sources)
	}
	if db := response.Images[1]; db.Service != "db" || db.NewVersion != "17.2" || db.EligibleAt == nil {
		t.Errorf("unexpected db update %+v", db)
	}
}

func TestApplicationResponse_ReleaseAgeDefaults(t *testing.T) {
	response := toApplicationResponse(&models.Application{})
	if response.ReleaseAge.MinimumSeconds != nil || response.ReleaseAge.AllowedRegistries == nil || response.ReleaseAge.ServiceOverrides == nil {
		t.Errorf("expected the global default with empty lists, got %+v", response.ReleaseAge)
	}
}

func TestWebhookImage(t *testing.T) {
	if got := dockerHubImage(dockerHubPayloadFrom(t, dockerHubPushBody)); got != "svendowideit/testhook:latest" {
		t.Errorf("dockerHubImage = %q", got)
	}

	tests := []struct {
		name, body, want string
	}{
		{"harbor", `{"type":"PUSH_ARTIFACT","event_data":{"resources":[{"resource_url":"harbor.local/library/nginx:1.27"}]}}`, "harbor.local/library/nginx:1.27"},
		{"harbor cloudevents", `{"type":"harbor.artifact.pushed","data":{"resources":[{"resource_url":"harbor.local/library/nginx:1.27"}]}}`, "harbor.local/library/nginx:1.27"},
		{"generic", `{}`, ""},
		{"empty", ``, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := harborImage([]byte(tt.body)); got != tt.want {
				t.Errorf("harborImage = %q, want %q", got, tt.want)
			}
		})
	}
}

func dockerHubPayloadFrom(t *testing.T, body string) dockerHubPayload {
	t.Helper()
	payload, ok := parseDockerHubPayload([]byte(body))
	if !ok {
		t.Fatalf("not a Docker Hub payload: %s", body)
	}
	return payload
}
