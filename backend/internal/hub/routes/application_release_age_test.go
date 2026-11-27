package routes

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/middleware"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/OrcaCD/orca-cd/internal/hub/websocket"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
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
		"/api/v1/applications/"+appID+"/release-age/settings", body, gin.Params{{Key: "id", Value: appID}})
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
		{app.Id, `{"minimumSeconds":0,"allowedRegistries":null}`, http.StatusBadRequest},
		{app.Id, `{"minimumSeconds":0,"allowedRegistries":` + tooManyRegistries() + `}`, http.StatusBadRequest},
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
	for _, body := range []string{`not json`, `{}`} {
		if w := setService("db", body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d", body, w.Code)
		}
	}

	w := invokeReleaseAgeHandler(t, UpdateServiceReleaseAgeHandler, http.MethodPut,
		"/api/v1/applications/missing/release-age/services/db", `{"minimumSeconds":0}`,
		gin.Params{{Key: "id", Value: "missing"}, {Key: "service", Value: "db"}})
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for an unknown application, got %d", w.Code)
	}
}

func tooManyRegistries() string {
	registries := make([]string, 51)
	for i := range registries {
		registries[i] = fmt.Sprintf("registry%d.local", i)
	}
	body, _ := json.Marshal(registries)
	return string(body)
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

	for _, body := range []string{
		`{"minimumSeconds":-1,"allowedRegistries":[]}`,
		`{"minimumSeconds":0,"allowedRegistries":["registry.local/library"]}`,
		`{}`,
		`{"minimumSeconds":null,"allowedRegistries":[]}`,
		`{"minimumSeconds":0}`,
		`{"minimumSeconds":0,"allowedRegistries":null}`,
		`not json`,
	} {
		w = invokeReleaseAgeHandler(t, AdminUpdateReleaseAgeSettingsHandler, http.MethodPut,
			"/api/v1/admin/settings/release-age", body, nil)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d", body, w.Code)
		}
	}
}

func TestAdminUpdateReleaseAgeSettingsHandler_SendsSettingsToAgents(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-release-age-global")
	agent := seedTestAgent(t, "agent-release-age-global")
	app := seedTestApplication(t, repo.Id, agent.Id, "Release Age Global App")
	nop := zerolog.Nop()
	hub := websocket.NewHub(&nop)
	websocket.DefaultHub = hub
	t.Cleanup(func() { websocket.DefaultHub = nil })
	client, err := hub.Register(agent.Id, nil)
	if err != nil {
		t.Fatalf("failed to register fake agent: %v", err)
	}

	w := invokeReleaseAgeHandler(t, AdminUpdateReleaseAgeSettingsHandler, http.MethodPut,
		"/api/v1/admin/settings/release-age", `{"minimumSeconds":3600,"allowedRegistries":[]}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// The application inherits the new default, so its agent gets it right away
	select {
	case msg := <-client.Send:
		settings := msg.GetAgentSettings().GetImagePollSettings()
		if len(settings) != 1 || settings[0].ApplicationId != app.Id || settings[0].GetReleaseAge().GetMinimumSeconds() != 3600 {
			t.Errorf("unexpected agent settings %v", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("expected the agent to get its settings")
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
		{ApplicationId: app.Id, Service: "web", Image: "ghcr.io/org/web:1", Status: models.ImageUpdateUpToDate, MinimumAgeSeconds: 86400, MinimumAgeEnforced: true, CheckedAt: checkedAt},
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
	if sources["web"] != "global" || sources["db"] != "service" || sources["cache"] != "global" {
		t.Errorf("age sources = %v", sources)
	}
	if db := response.Images[1]; db.Service != "db" || db.NewVersion != "17.2" || db.EligibleAt == nil {
		t.Errorf("unexpected db update %+v", db)
	}
}

func TestListApplicationImageUpdatesHandler_OwnAgeAndUnknownApplication(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-image-updates-own")
	agent := seedTestAgent(t, "agent-image-updates-own")
	app := seedTestApplication(t, repo.Id, agent.Id, "Image Updates Own App")
	own := int64(3600)
	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).
		Select("MinimumReleaseAgeSeconds").
		Updates(t.Context(), models.Application{MinimumReleaseAgeSeconds: &own}); err != nil {
		t.Fatalf("update application: %v", err)
	}

	// Before the first image check there is nothing to show
	w := invokeReleaseAgeHandler(t, ListApplicationImageUpdatesHandler, http.MethodGet,
		"/api/v1/applications/"+app.Id+"/image-updates", "", gin.Params{{Key: "id", Value: app.Id}})
	var response imageUpdatesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if w.Code != http.StatusOK || response.CheckedAt != nil || response.Images == nil || len(response.Images) != 0 {
		t.Fatalf("expected an empty list, got %d: %s", w.Code, w.Body.String())
	}

	row := models.ApplicationImageUpdate{ApplicationId: app.Id, Service: "web", Image: "nginx:1", Status: models.ImageUpdateUpToDate, MinimumAgeSeconds: own, CheckedAt: time.Now().UTC()}
	if err := gorm.G[models.ApplicationImageUpdate](db.DB).Create(t.Context(), &row); err != nil {
		t.Fatalf("seed image update: %v", err)
	}
	w = invokeReleaseAgeHandler(t, ListApplicationImageUpdatesHandler, http.MethodGet,
		"/api/v1/applications/"+app.Id+"/image-updates", "", gin.Params{{Key: "id", Value: app.Id}})
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Images) != 1 || response.Images[0].AgeSource != "application" {
		t.Errorf("expected the age of the application, got %+v", response.Images)
	}

	w = invokeReleaseAgeHandler(t, ListApplicationImageUpdatesHandler, http.MethodGet,
		"/api/v1/applications/missing/image-updates", "", gin.Params{{Key: "id", Value: "missing"}})
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for an unknown application, got %d", w.Code)
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
	otherRegistry := `{"push_data":{"tag":"17"},"repository":{"repo_name":"ghcr.io/org/postgres"}}`
	if got := dockerHubImage(dockerHubPayloadFrom(t, otherRegistry)); got != "" {
		t.Errorf("expected no image of another registry, got %q", got)
	}

	tests := []struct {
		image, registry, want string
	}{
		{"ghcr.io/org/app:1.0", "ghcr.io", "ghcr.io/org/app:1.0"},
		{"docker.io/library/postgres:17", "ghcr.io", ""},
		{"postgres:17", "docker.io", "postgres:17"},
		{"registry.local/postgres:17", "docker.io", ""},
		{"not a reference", "ghcr.io", ""},
		{"", "ghcr.io", ""},
	}
	for _, tt := range tests {
		if got := announcedImage(tt.image, tt.registry); got != tt.want {
			t.Errorf("announcedImage(%q, %q) = %q, want %q", tt.image, tt.registry, got, tt.want)
		}
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

func TestReleaseAgeHandlers_GlobalMinimumIsLowerBound(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-release-age-floor")
	agent := seedTestAgent(t, "agent-release-age-floor")
	app := seedTestApplication(t, repo.Id, agent.Id, "Release Age Floor App")
	if w := invokeReleaseAgeHandler(t, AdminUpdateReleaseAgeSettingsHandler, http.MethodPut,
		"/api/v1/admin/settings/release-age", `{"minimumSeconds":86400,"allowedRegistries":[]}`, nil); w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if w := invokeReleaseAgeSettingsHandler(t, app.Id, `{"minimumSeconds":3600,"allowedRegistries":[]}`); w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 below the global minimum, got %d", w.Code)
	}
	decodeApplication(t, invokeReleaseAgeSettingsHandler(t, app.Id, `{"minimumSeconds":172800,"allowedRegistries":[]}`))

	setService := func(body string) *httptest.ResponseRecorder {
		return invokeReleaseAgeHandler(t, UpdateServiceReleaseAgeHandler, http.MethodPut,
			"/api/v1/applications/"+app.Id+"/release-age/services/web", body,
			gin.Params{{Key: "id", Value: app.Id}, {Key: "service", Value: "web"}})
	}
	if w := setService(`{"minimumSeconds":0}`); w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for a service below the global minimum, got %d", w.Code)
	}
	decodeApplication(t, setService(`{"minimumSeconds":86400}`))
}

func TestUpdateServiceReleaseAgeHandler_LimitsOverrides(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-release-age-limit")
	agent := seedTestAgent(t, "agent-release-age-limit")
	app := seedTestApplication(t, repo.Id, agent.Id, "Release Age Limit App")
	overrides := make(map[string]int64, maxServiceReleaseAgeOverrides)
	for i := range maxServiceReleaseAgeOverrides {
		overrides[fmt.Sprintf("service-%d", i)] = 0
	}
	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).
		Select("ReleaseAgeServiceOverrides").
		Updates(t.Context(), models.Application{ReleaseAgeServiceOverrides: overrides}); err != nil {
		t.Fatalf("update application: %v", err)
	}
	setService := func(service string) *httptest.ResponseRecorder {
		return invokeReleaseAgeHandler(t, UpdateServiceReleaseAgeHandler, http.MethodPut,
			"/api/v1/applications/"+app.Id+"/release-age/services/"+service, `{"minimumSeconds":60}`,
			gin.Params{{Key: "id", Value: app.Id}, {Key: "service", Value: service}})
	}

	if w := setService("one-too-many"); w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 above the limit, got %d", w.Code)
	}
	// An existing override can still be changed.
	decodeApplication(t, setService("service-0"))
}

func TestUpdateServiceReleaseAgeHandler_ConcurrentUpdatesKeepEveryService(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-release-age-concurrent")
	agent := seedTestAgent(t, "agent-release-age-concurrent")
	app := seedTestApplication(t, repo.Id, agent.Id, "Release Age Concurrent App")

	const services = 10
	var wg sync.WaitGroup
	for i := range services {
		wg.Go(func() {
			service := fmt.Sprintf("service-%d", i)
			invokeReleaseAgeHandler(t, UpdateServiceReleaseAgeHandler, http.MethodPut,
				"/api/v1/applications/"+app.Id+"/release-age/services/"+service, `{"minimumSeconds":60}`,
				gin.Params{{Key: "id", Value: app.Id}, {Key: "service", Value: service}})
		})
	}
	wg.Wait()

	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(t.Context())
	if err != nil {
		t.Fatalf("load application: %v", err)
	}
	if len(updated.ReleaseAgeServiceOverrides) != services {
		t.Errorf("expected %d overrides, got %v", services, updated.ReleaseAgeServiceOverrides)
	}
}

func TestReleaseAgeHandlers_GlobalSettingsError(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-release-age-error")
	agent := seedTestAgent(t, "agent-release-age-error")
	app := seedTestApplication(t, repo.Id, agent.Id, "Release Age Error App")
	if err := db.DB.Migrator().DropTable(&models.Settings{}); err != nil {
		t.Fatalf("drop settings: %v", err)
	}

	if w := invokeReleaseAgeSettingsHandler(t, app.Id, `{"minimumSeconds":3600,"allowedRegistries":[]}`); w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
	w := invokeReleaseAgeHandler(t, UpdateServiceReleaseAgeHandler, http.MethodPut,
		"/api/v1/applications/"+app.Id+"/release-age/services/web", `{"minimumSeconds":60}`,
		gin.Params{{Key: "id", Value: app.Id}, {Key: "service", Value: "web"}})
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
	if w := invokeReleaseAgeHandler(t, GetReleaseAgeSettingsHandler, http.MethodGet, "/api/v1/settings/release-age", "", nil); w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestAgeSource_Enforced(t *testing.T) {
	row := &models.ApplicationImageUpdate{Service: "web", MinimumAgeEnforced: true}
	app := &models.Application{ReleaseAgeServiceOverrides: map[string]int64{"web": 0}}
	if got := ageSource(row, app, false); got != "global" {
		t.Errorf("ageSource = %q, want global", got)
	}
	row.MinimumAgeEnforced = false
	if got := ageSource(row, app, false); got != "service" {
		t.Errorf("ageSource = %q, want service", got)
	}
}

func TestAdminUpdateReleaseAgeSettingsHandler_RequiresAdmin(t *testing.T) {
	setupTestDBWithApplications(t)
	user := &models.User{Id: "user-release-age"}
	for _, tt := range []struct {
		name  string
		perms *rbac.Permissions
		want  int
	}{
		{"user", rbac.New(nil), http.StatusForbidden},
		{"admin", rbac.Admin(), http.StatusOK},
	} {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(func(c *gin.Context) {
				auth.SetClaims(c, &auth.UserClaims{Subject: user.Id})
				auth.SetUser(c, user)
				rbac.Set(c, tt.perms)
				c.Next()
			})
			router.PUT("/api/v1/admin/settings/release-age", middleware.RequireAdmin(), AdminUpdateReleaseAgeSettingsHandler)

			req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/release-age",
				strings.NewReader(`{"minimumSeconds":0,"allowedRegistries":[]}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tt.want {
				t.Errorf("expected %d, got %d: %s", tt.want, w.Code, w.Body.String())
			}
		})
	}
}

// Raising the global minimum raises the stored ages below it, so the settings
// show the age that applies and can be saved as they are.
func TestAdminUpdateReleaseAgeSettingsHandler_RaisesStoredAgesBelowTheMinimum(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-release-age-raise")
	agent := seedTestAgent(t, "agent-release-age-raise")
	below := seedTestApplication(t, repo.Id, agent.Id, "Release Age Below App")
	above := seedTestApplication(t, repo.Id, agent.Id, "Release Age Above App")
	decodeApplication(t, invokeReleaseAgeSettingsHandler(t, below.Id, `{"minimumSeconds":172800,"allowedRegistries":[]}`))
	decodeApplication(t, invokeReleaseAgeSettingsHandler(t, above.Id, `{"minimumSeconds":864000,"allowedRegistries":[]}`))
	for service, seconds := range map[string]int{"web": 86400, "db": 1209600} {
		decodeApplication(t, invokeReleaseAgeHandler(t, UpdateServiceReleaseAgeHandler, http.MethodPut,
			"/api/v1/applications/"+below.Id+"/release-age/services/"+service, fmt.Sprintf(`{"minimumSeconds":%d}`, seconds),
			gin.Params{{Key: "id", Value: below.Id}, {Key: "service", Value: service}}))
	}

	if w := invokeReleaseAgeHandler(t, AdminUpdateReleaseAgeSettingsHandler, http.MethodPut,
		"/api/v1/admin/settings/release-age", `{"minimumSeconds":604800,"allowedRegistries":[]}`, nil); w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	load := func(id string) models.Application {
		t.Helper()
		app, err := gorm.G[models.Application](db.DB).Where("id = ?", id).First(t.Context())
		if err != nil {
			t.Fatalf("load application: %v", err)
		}
		return app
	}
	got := load(below.Id)
	if got.MinimumReleaseAgeSeconds != nil {
		t.Errorf("expected the application to use the global minimum, got %d", *got.MinimumReleaseAgeSeconds)
	}
	if want := map[string]int64{"web": 604800, "db": 1209600}; !maps.Equal(got.ReleaseAgeServiceOverrides, want) {
		t.Errorf("service overrides = %v, want %v", got.ReleaseAgeServiceOverrides, want)
	}
	if got := load(above.Id); got.MinimumReleaseAgeSeconds == nil || *got.MinimumReleaseAgeSeconds != 864000 {
		t.Errorf("an age above the minimum must stay, got %v", got.MinimumReleaseAgeSeconds)
	}

	// The stored settings can be saved again unchanged.
	decodeApplication(t, invokeReleaseAgeSettingsHandler(t, below.Id, `{"minimumSeconds":null,"allowedRegistries":[]}`))
}
