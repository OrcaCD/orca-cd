package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/middleware"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/OrcaCD/orca-cd/internal/hub/websocket"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

const testContainerID = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type containerTestAgent struct {
	mu          sync.Mutex
	listReqs    []*messages.ContainerListRequest
	inspectReqs []*messages.ContainerInspectRequest
}

func (a *containerTestAgent) inspects() []*messages.ContainerInspectRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]*messages.ContainerInspectRequest(nil), a.inspectReqs...)
}

func (a *containerTestAgent) lists() []*messages.ContainerListRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]*messages.ContainerListRequest(nil), a.listReqs...)
}

func withContainerAgent(t *testing.T, agentID string, capabilities []string, list *messages.ContainerListResult, inspect *messages.ContainerInspectResult) *containerTestAgent {
	t.Helper()
	nop := zerolog.Nop()
	hub := websocket.NewHub(&nop)
	websocket.DefaultHub = hub
	t.Cleanup(func() { websocket.DefaultHub = nil })

	client, err := hub.Register(agentID, nil)
	if err != nil {
		t.Fatalf("failed to register fake agent: %v", err)
	}
	client.SetCapabilities(capabilities)

	agent := &containerTestAgent{}
	go func() {
		for msg := range client.Send {
			if req := msg.GetContainerListRequest(); req != nil {
				agent.mu.Lock()
				agent.listReqs = append(agent.listReqs, req)
				agent.mu.Unlock()
				result := list
				result.RequestId, result.ApplicationId = req.RequestId, req.ApplicationId
				hub.ResolveContainerListResult(agentID, result)
			}
			if req := msg.GetContainerInspectRequest(); req != nil {
				agent.mu.Lock()
				agent.inspectReqs = append(agent.inspectReqs, req)
				agent.mu.Unlock()
				result := inspect
				result.RequestId, result.ApplicationId = req.RequestId, req.ApplicationId
				hub.ResolveContainerInspectResult(agentID, result)
			}
		}
	}()
	t.Cleanup(client.Close)
	return agent
}

var monitoringCapabilities = []string{websocket.CapabilityContainerMonitoring}

func setupContainerTest(t *testing.T) models.Application {
	t.Helper()
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-containers")
	agent := seedTestAgent(t, "agent-containers")
	return seedTestApplication(t, repo.Id, agent.Id, "Container App")
}

func containerRequest(t *testing.T, handler gin.HandlerFunc, appID, containerID string) *httptest.ResponseRecorder {
	t.Helper()
	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/applications/"+appID+"/containers", nil)
	c.Params = gin.Params{{Key: "id", Value: appID}, {Key: "containerId", Value: containerID}}
	handler(c)
	return w
}

func TestListApplicationContainersHandler_Success(t *testing.T) {
	app := setupContainerTest(t)
	agent := withContainerAgent(t, app.AgentId, monitoringCapabilities, &messages.ContainerListResult{
		Success: true,
		Containers: []*messages.ContainerInfo{{
			Id:              testContainerID,
			Name:            "app-web-1",
			Service:         "web",
			State:           "running",
			Health:          messages.HealthStatus_HEALTH_STATUS_UNHEALTHY,
			HealthRaw:       "unhealthy",
			Image:           "nginx:1.27",
			ImageDigest:     "nginx@sha256:abc",
			RestartCount:    3,
			StartedAtMs:     1_700_000_000_000,
			ContainerNumber: 1,
			CpuLimit:        0.5,
			Ports:           []*messages.PortBinding{{Ip: "0.0.0.0", PublicPort: 8080, PrivatePort: 80, Protocol: "tcp"}},
			Networks:        []*messages.NetworkInfo{{Name: "app_default", IpAddress: "172.18.0.2", Aliases: []string{"web"}}},
			Mounts:          []*messages.MountInfo{{Type: "volume", Name: "data", Destination: "/data", ReadOnly: true}},
		}},
	}, nil)

	w := containerRequest(t, ListApplicationContainersHandler, app.Id, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body struct {
		Containers []containerResponse `json:"containers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(body.Containers) != 1 {
		t.Fatalf("expected one container, got %+v", body)
	}
	got := body.Containers[0]
	if got.Id != testContainerID || got.Service != "web" || got.State != "running" || got.Health != models.Unhealthy {
		t.Errorf("unexpected container: %+v", got)
	}
	if got.RestartCount != 3 || got.StartedAt == nil || got.StartedAt.UnixMilli() != 1_700_000_000_000 || got.FinishedAt != nil {
		t.Errorf("unexpected timing: %+v", got)
	}
	if len(got.Ports) != 1 || got.Ports[0].PublicPort != 8080 || len(got.Networks) != 1 || len(got.Mounts) != 1 || !got.Mounts[0].ReadOnly {
		t.Errorf("unexpected ports, networks or mounts: %+v", got)
	}
	if got.CpuLimit != 0.5 || got.ContainerNumber != 1 {
		t.Errorf("unexpected limits: %+v", got)
	}

	reqs := agent.lists()
	if len(reqs) != 1 || reqs[0].ApplicationId != app.Id {
		t.Fatalf("expected one list request for the application, got %+v", reqs)
	}
}

func TestListApplicationContainersHandler_NoContainersIsEmptyArray(t *testing.T) {
	app := setupContainerTest(t)
	withContainerAgent(t, app.AgentId, monitoringCapabilities, &messages.ContainerListResult{Success: true}, nil)

	w := containerRequest(t, ListApplicationContainersHandler, app.Id, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"containers":[]`) {
		t.Fatalf("expected empty containers array, got %s", w.Body.String())
	}
}

func TestListApplicationContainersHandler_Errors(t *testing.T) {
	t.Run("application not found", func(t *testing.T) {
		setupTestDBWithApplications(t)
		withOfflineHub(t)

		w := containerRequest(t, ListApplicationContainersHandler, "missing", "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("agent offline", func(t *testing.T) {
		app := setupContainerTest(t)
		withOfflineHub(t)

		w := containerRequest(t, ListApplicationContainersHandler, app.Id, "")
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("agent without the capability", func(t *testing.T) {
		for _, capabilities := range [][]string{nil, {"something-else"}} {
			app := setupContainerTest(t)
			agent := withContainerAgent(t, app.AgentId, capabilities, &messages.ContainerListResult{Success: true}, nil)

			w := containerRequest(t, ListApplicationContainersHandler, app.Id, "")
			if w.Code != http.StatusUnprocessableEntity {
				t.Fatalf("capabilities %v: expected 422, got %d: %s", capabilities, w.Code, w.Body.String())
			}
			if len(agent.lists()) != 0 {
				t.Fatalf("capabilities %v: request must not reach an agent without the capability", capabilities)
			}
		}
	})

	t.Run("agent failure", func(t *testing.T) {
		app := setupContainerTest(t)
		withContainerAgent(t, app.AgentId, monitoringCapabilities, &messages.ContainerListResult{ErrorMessage: "docker daemon unreachable"}, nil)

		w := containerRequest(t, ListApplicationContainersHandler, app.Id, "")
		if w.Code != http.StatusBadGateway {
			t.Fatalf("expected 502, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Docker daemon unreachable") {
			t.Fatalf("expected agent error in body, got %s", w.Body.String())
		}
	})

	t.Run("timeout", func(t *testing.T) {
		app := setupContainerTest(t)
		nop := zerolog.Nop()
		hub := websocket.NewHub(&nop)
		websocket.DefaultHub = hub
		t.Cleanup(func() { websocket.DefaultHub = nil })
		client, err := hub.Register(app.AgentId, nil)
		if err != nil {
			t.Fatalf("register agent: %v", err)
		}
		client.SetCapabilities(monitoringCapabilities)

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		c, w := makeAuthContext(t, "user-1")
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
		c.Params = gin.Params{{Key: "id", Value: app.Id}}
		ListApplicationContainersHandler(c)

		if w.Code != http.StatusGatewayTimeout {
			t.Fatalf("expected 504, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestInspectApplicationContainerHandler_Success(t *testing.T) {
	app := setupContainerTest(t)
	const inspectJSON = "{\n  \"Config\": {\n    \"Image\": \"nginx\"\n  }\n}"
	agent := withContainerAgent(t, app.AgentId, monitoringCapabilities, nil, &messages.ContainerInspectResult{Success: true, InspectJson: inspectJSON})

	w := containerRequest(t, InspectApplicationContainerHandler, app.Id, testContainerID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body struct {
		InspectJson string `json:"inspectJson"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.InspectJson != inspectJSON {
		t.Errorf("expected the inspect JSON to be passed on unchanged, got %q", body.InspectJson)
	}

	reqs := agent.inspects()
	if len(reqs) != 1 || reqs[0].ApplicationId != app.Id || reqs[0].ContainerId != testContainerID {
		t.Fatalf("expected one inspect request for the container, got %+v", reqs)
	}
}

func TestInspectApplicationContainerHandler_InvalidContainerID(t *testing.T) {
	app := setupContainerTest(t)
	agent := withContainerAgent(t, app.AgentId, monitoringCapabilities, nil, &messages.ContainerInspectResult{Success: true})

	for _, id := range []string{
		"",
		"abc",
		"0123456789a",
		testContainerID + "0",
		"0123456789ABCDEF",
		"0123456789ab..",
		"../../etc/passwd",
		"0123456789ab\n",
		"app-web-1",
	} {
		w := containerRequest(t, InspectApplicationContainerHandler, app.Id, id)
		if w.Code != http.StatusBadRequest {
			t.Errorf("container id %q: expected 400, got %d: %s", id, w.Code, w.Body.String())
		}
	}
	if len(agent.inspects()) != 0 {
		t.Fatal("an invalid container id must not reach the agent")
	}
}

func TestInspectApplicationContainerHandler_ShortID(t *testing.T) {
	app := setupContainerTest(t)
	withContainerAgent(t, app.AgentId, monitoringCapabilities, nil, &messages.ContainerInspectResult{Success: true, InspectJson: "{}"})

	w := containerRequest(t, InspectApplicationContainerHandler, app.Id, testContainerID[:12])
	if w.Code != http.StatusOK {
		t.Fatalf("expected a short id to be accepted, got %d: %s", w.Code, w.Body.String())
	}
}

func TestInspectApplicationContainerHandler_Errors(t *testing.T) {
	t.Run("container not found", func(t *testing.T) {
		app := setupContainerTest(t)
		withContainerAgent(t, app.AgentId, monitoringCapabilities, nil, &messages.ContainerInspectResult{ErrorMessage: "container not found"})

		w := containerRequest(t, InspectApplicationContainerHandler, app.Id, testContainerID)
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("agent failure", func(t *testing.T) {
		app := setupContainerTest(t)
		withContainerAgent(t, app.AgentId, monitoringCapabilities, nil, &messages.ContainerInspectResult{ErrorMessage: "docker daemon unreachable"})

		w := containerRequest(t, InspectApplicationContainerHandler, app.Id, testContainerID)
		if w.Code != http.StatusBadGateway {
			t.Fatalf("expected 502, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("agent offline", func(t *testing.T) {
		app := setupContainerTest(t)
		withOfflineHub(t)

		w := containerRequest(t, InspectApplicationContainerHandler, app.Id, testContainerID)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("agent without the capability", func(t *testing.T) {
		app := setupContainerTest(t)
		agent := withContainerAgent(t, app.AgentId, nil, nil, &messages.ContainerInspectResult{Success: true})

		w := containerRequest(t, InspectApplicationContainerHandler, app.Id, testContainerID)
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
		}
		if len(agent.inspects()) != 0 {
			t.Fatal("request must not reach an agent without the capability")
		}
	})
}

// containerRouter registers the routes with the permission checks of RegisterRoutes.
func containerRouter(perms *rbac.Permissions) *gin.Engine {
	router := gin.New()
	router.Use(func(c *gin.Context) {
		auth.SetUser(c, &models.User{Id: "user-1", Name: "Test User", Email: "test@example.com"})
		rbac.Set(c, perms)
		c.Next()
	})
	app := func(action models.PermissionAction) gin.HandlerFunc {
		return middleware.RequirePermission(models.ResourceApplication, action)
	}
	router.GET("/applications/:id/containers", app(models.ActionRead), ListApplicationContainersHandler)
	router.GET("/applications/:id/containers/:containerId/inspect", app(models.ActionInspect), InspectApplicationContainerHandler)
	return router
}

func TestContainerRoutes_Permissions(t *testing.T) {
	app := setupContainerTest(t)
	withContainerAgent(t, app.AgentId, monitoringCapabilities,
		&messages.ContainerListResult{Success: true},
		&messages.ContainerInspectResult{Success: true, InspectJson: "{}"})

	listPath := "/applications/" + app.Id + "/containers"
	inspectPath := listPath + "/" + testContainerID + "/inspect"

	tokenOwner := rbac.New([]models.RolePermission{grantAll(models.ResourceApplication, models.ActionInspect)})

	tests := []struct {
		name        string
		perms       *rbac.Permissions
		wantList    int
		wantInspect int
	}{
		{"no permissions", rbac.New(nil), http.StatusForbidden, http.StatusForbidden},
		{"read only", rbac.New([]models.RolePermission{grantAll(models.ResourceApplication, models.ActionRead)}), http.StatusOK, http.StatusForbidden},
		{"every other action", rbac.New([]models.RolePermission{
			grantAll(models.ResourceApplication, models.ActionCreate),
			grantAll(models.ResourceApplication, models.ActionUpdate),
			grantAll(models.ResourceApplication, models.ActionDelete),
			grantAll(models.ResourceApplication, models.ActionDeploy),
			grantAll(models.ResourceApplication, models.ActionPullImages),
			grantAll(models.ResourceApplication, models.ActionRollback),
		}), http.StatusOK, http.StatusForbidden},
		{"inspect implies read", tokenOwner, http.StatusOK, http.StatusOK},
		{"inspect on this application", rbac.New([]models.RolePermission{grantOn(models.ResourceApplication, models.ActionInspect, app.Id)}), http.StatusOK, http.StatusOK},
		{"inspect on another application", rbac.New([]models.RolePermission{grantOn(models.ResourceApplication, models.ActionInspect, "other")}), http.StatusForbidden, http.StatusForbidden},
		{"admin", rbac.Admin(), http.StatusOK, http.StatusOK},
		{
			"token with the read scope of an owner who can inspect",
			tokenOwner.Restrict([]models.APITokenScope{"application:read"}),
			http.StatusOK, http.StatusForbidden,
		},
		{
			"token with the inspect scope",
			tokenOwner.Restrict([]models.APITokenScope{"application:inspect"}),
			http.StatusOK, http.StatusOK,
		},
		{
			"token with the inspect scope of an owner who cannot inspect",
			rbac.New([]models.RolePermission{grantAll(models.ResourceApplication, models.ActionRead)}).Restrict([]models.APITokenScope{"application:inspect"}),
			http.StatusForbidden, http.StatusForbidden,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := containerRouter(tt.perms)
			for path, want := range map[string]int{listPath: tt.wantList, inspectPath: tt.wantInspect} {
				w := httptest.NewRecorder()
				router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
				if w.Code != want {
					t.Errorf("GET %s: expected %d, got %d: %s", path, want, w.Code, w.Body.String())
				}
			}
		})
	}
}
