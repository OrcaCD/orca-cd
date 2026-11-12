package routes

import (
	"bytes"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func grantAll(resource models.PermissionResource, action models.PermissionAction) models.RolePermission {
	return models.RolePermission{Resource: resource, Action: action}
}

func grantOn(resource models.PermissionResource, action models.PermissionAction, id string) models.RolePermission {
	return models.RolePermission{Resource: resource, Action: action, ResourceId: &id}
}

// makePermissionContext returns a request context for a user with the given grants.
func makePermissionContext(t *testing.T, method, path string, body any, grants ...models.RolePermission) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	c, w := makeAuthContext(t, "user-1")
	rbac.Set(c, rbac.New(grants))

	var reader *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal body: %v", err)
		}
		reader = bytes.NewReader(data)
	} else {
		reader = bytes.NewReader(nil)
	}
	c.Request = httptest.NewRequest(method, path, reader)
	c.Request.Header.Set("Content-Type", "application/json")
	return c, w
}

func TestListApplicationsHandler_OnlyReadableApplications(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-visible")
	agent := seedTestAgent(t, "agent-visible")
	visible := seedTestApplication(t, repo.Id, agent.Id, "Visible")
	seedTestApplication(t, repo.Id, agent.Id, "Hidden")

	tests := []struct {
		name   string
		grants []models.RolePermission
		want   []string
	}{
		{"no permissions", nil, []string{}},
		{"single application", []models.RolePermission{grantOn(models.ResourceApplication, models.ActionRead, visible.Id)}, []string{visible.Id}},
		{"implied by deploy", []models.RolePermission{grantOn(models.ResourceApplication, models.ActionDeploy, visible.Id)}, []string{visible.Id}},
		{"other resource type", []models.RolePermission{grantAll(models.ResourceRepository, models.ActionRead)}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, w := makePermissionContext(t, http.MethodGet, "/api/v1/applications", nil, tt.grants...)
			ListApplicationsHandler(c)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}
			var body []applicationListResponse
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			ids := []string{}
			for _, app := range body {
				ids = append(ids, app.Id)
			}
			if !slices.Equal(ids, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, ids)
			}
		})
	}
}

func TestListRepositoriesAndAgentsHandler_OnlyReadable(t *testing.T) {
	setupTestDBWithApplications(t)
	visibleRepo := seedTestRepository(t, "https://github.com/owner/visible")
	seedTestRepository(t, "https://github.com/owner/hidden")
	visibleAgent := seedTestAgent(t, "visible")
	seedTestAgent(t, "hidden")

	grants := []models.RolePermission{
		grantOn(models.ResourceRepository, models.ActionUpdate, visibleRepo.Id),
		grantOn(models.ResourceAgent, models.ActionRead, visibleAgent.Id),
	}

	c, w := makePermissionContext(t, http.MethodGet, "/api/v1/repositories", nil, grants...)
	ListRepositoriesHandler(c)
	var repos []repositoryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &repos); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(repos) != 1 || repos[0].Id != visibleRepo.Id {
		t.Fatalf("expected only the visible repository, got %+v", repos)
	}

	c, w = makePermissionContext(t, http.MethodGet, "/api/v1/agents", nil, grants...)
	ListAgentsHandler(c)
	var agents []agentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &agents); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(agents) != 1 || agents[0].Id != visibleAgent.Id {
		t.Fatalf("expected only the visible agent, got %+v", agents)
	}
}

func TestCreateApplicationHandler_RequiresReadOnRepositoryAndAgent(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-deps")
	agent := seedTestAgent(t, "agent-deps")
	body := map[string]any{
		"name":         "App",
		"repositoryId": repo.Id,
		"agentId":      agent.Id,
		"branch":       "main",
		"path":         "deploy.yml",
	}
	create := grantAll(models.ResourceApplication, models.ActionCreate)

	tests := []struct {
		name   string
		grants []models.RolePermission
		want   string
	}{
		{"without repository", []models.RolePermission{create, grantAll(models.ResourceAgent, models.ActionRead)}, "insufficient permissions for the repository"},
		{"without agent", []models.RolePermission{create, grantOn(models.ResourceRepository, models.ActionRead, repo.Id)}, "insufficient permissions for the agent"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, w := makePermissionContext(t, http.MethodPost, "/api/v1/applications", body, tt.grants...)
			CreateApplicationHandler(c)

			if w.Code != http.StatusForbidden || !bytes.Contains(w.Body.Bytes(), []byte(tt.want)) {
				t.Fatalf("expected 403 %q, got %d: %s", tt.want, w.Code, w.Body.String())
			}
		})
	}

	count, err := gorm.G[models.Application](db.DB).Count(t.Context(), "*")
	if err != nil || count != 0 {
		t.Fatalf("expected no application to be created, count=%d err=%v", count, err)
	}
}

func TestDeleteRepositoryHandler_RequiresDeleteOnItsApplications(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-cascade")
	agent := seedTestAgent(t, "agent-cascade")
	seedTestApplication(t, repo.Id, agent.Id, "Cascaded")

	c, w := makePermissionContext(t, http.MethodDelete, "/api/v1/repositories/"+repo.Id, nil,
		grantAll(models.ResourceRepository, models.ActionDelete))
	c.Params = gin.Params{{Key: "id", Value: repo.Id}}
	DeleteRepositoryHandler(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if exists, _ := hasRecord[models.Repository](c, repo.Id); !exists {
		t.Fatal("expected the repository to remain")
	}
}

func TestPruneGrants_RemovesGrantsOnDeletedResources(t *testing.T) {
	setupTestDBWithApplications(t)
	if err := db.DB.AutoMigrate(&models.Notification{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	repo := seedTestRepository(t, "https://github.com/owner/repo-prune")
	agent := seedTestAgent(t, "agent-prune")
	kept := seedTestApplication(t, repo.Id, agent.Id, "Kept")

	grants := []models.RolePermission{
		grantOn(models.ResourceApplication, models.ActionRead, kept.Id),
		grantOn(models.ResourceApplication, models.ActionRead, "deleted-app"),
		grantAll(models.ResourceApplication, models.ActionDeploy),
	}
	for i := range grants {
		grants[i].RoleId = rbac.UserRoleID
	}
	if _, err := gorm.G[models.RolePermission](db.DB).Where("role_id = ?", rbac.UserRoleID).Delete(t.Context()); err != nil {
		t.Fatalf("failed to clear grants: %v", err)
	}
	if err := gorm.G[models.RolePermission](db.DB).CreateInBatches(t.Context(), &grants, 10); err != nil {
		t.Fatalf("failed to create grants: %v", err)
	}

	if pruned, err := rbac.PruneGrants(t.Context(), db.DB); err != nil || !slices.Equal(pruned, []string{rbac.UserRoleID}) {
		t.Fatalf("PruneGrants() = %v, %v; want the user role", pruned, err)
	}

	remaining, err := gorm.G[models.RolePermission](db.DB).Where("role_id = ?", rbac.UserRoleID).Find(t.Context())
	if err != nil {
		t.Fatalf("failed to load grants: %v", err)
	}
	if len(remaining) != 2 {
		t.Fatalf("expected the grant on the deleted application to be removed, got %+v", remaining)
	}
	for _, g := range remaining {
		if g.ResourceId != nil && *g.ResourceId == "deleted-app" {
			t.Fatal("expected the grant on the deleted application to be removed")
		}
	}
}

func TestNotificationTargets_RequireReadAccess(t *testing.T) {
	setupTestDBWithNotifications(t)
	repo := seedNotificationRepository(t)
	agent := seedNotificationAgent(t)
	visible := seedNotificationApplication(t, repo.Id, agent.Id, "Visible")
	hidden := seedNotificationApplication(t, repo.Id, agent.Id, "Hidden")

	grants := []models.RolePermission{
		grantAll(models.ResourceNotification, models.ActionCreate),
		grantAll(models.ResourceNotification, models.ActionUpdate),
		grantOn(models.ResourceApplication, models.ActionRead, visible.Id),
	}
	newBody := func(extra map[string]any) map[string]any {
		body := map[string]any{
			"name":            "Alerts",
			"type":            "discord",
			"config":          validDiscordConfig,
			"enabled":         true,
			"allApplications": false,
			"allAgents":       false,
			"allRepositories": false,
			"events":          []string{string(models.NotificationEventDeploymentFailed)},
		}
		maps.Copy(body, extra)
		return body
	}

	t.Run("create with a hidden application", func(t *testing.T) {
		c, w := makePermissionContext(t, http.MethodPost, "/api/v1/notifications", newBody(map[string]any{"applicationIds": []string{hidden.Id}}), grants...)
		CreateNotificationHandler(c)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("create for all applications", func(t *testing.T) {
		c, w := makePermissionContext(t, http.MethodPost, "/api/v1/notifications", newBody(map[string]any{"allApplications": true}), grants...)
		CreateNotificationHandler(c)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("update a notification about visible applications", func(t *testing.T) {
		notification := createNotificationRecord(t, []string{visible.Id})

		c, w := makePermissionContext(t, http.MethodPut, "/api/v1/notifications/"+notification.Id, newBody(map[string]any{"applicationIds": []string{}}), grants...)
		c.Params = gin.Params{{Key: "id", Value: notification.Id}}
		UpdateNotificationHandler(c)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("change a notification about a hidden application", func(t *testing.T) {
		notification := createNotificationRecord(t, []string{visible.Id, hidden.Id})

		c, w := makePermissionContext(t, http.MethodPut, "/api/v1/notifications/"+notification.Id, newBody(map[string]any{"applicationIds": []string{visible.Id}}), grants...)
		c.Params = gin.Params{{Key: "id", Value: notification.Id}}
		UpdateNotificationHandler(c)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 on update, got %d: %s", w.Code, w.Body.String())
		}

		c, w = makePermissionContext(t, http.MethodPut, "/api/v1/notifications/"+notification.Id+"/enabled", map[string]any{"enabled": false}, grants...)
		c.Params = gin.Params{{Key: "id", Value: notification.Id}}
		SetNotificationEnabledHandler(c)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 on enable, got %d: %s", w.Code, w.Body.String())
		}

		stored, err := getNotificationById(t.Context(), notification.Id)
		if err != nil {
			t.Fatalf("failed to load notification: %v", err)
		}
		if ids := sortedIds(stored.Applications, applicationIdOf); len(ids) != 2 || !stored.Enabled {
			t.Fatalf("expected the notification to be unchanged, got %v enabled=%v", ids, stored.Enabled)
		}
	})
}

func TestUpdateApplicationHandler_OnlyChangedSourcesNeedRead(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-update")
	agent := seedTestAgent(t, "agent-update")
	other := seedTestAgent(t, "agent-other")
	app := seedTestApplication(t, repo.Id, agent.Id, "Scoped")
	update := grantOn(models.ResourceApplication, models.ActionUpdate, app.Id)
	deploy := grantOn(models.ResourceApplication, models.ActionDeploy, app.Id)
	readAll := []models.RolePermission{update, grantAll(models.ResourceAgent, models.ActionRead), grantAll(models.ResourceRepository, models.ActionRead)}

	body := func(name, agentId, path string) map[string]any {
		return map[string]any{"name": name, "repositoryId": repo.Id, "agentId": agentId, "branch": "main", "path": path}
	}
	tests := []struct {
		name      string
		body      map[string]any
		grants    []models.RolePermission
		want      string
		forbidden bool
	}{
		{"same sources", body(app.Name.String(), agent.Id, app.Path), []models.RolePermission{update}, "", false},
		{"other agent", body(app.Name.String(), other.Id, app.Path), []models.RolePermission{update}, "insufficient permissions for the agent", true},
		{"other path", body(app.Name.String(), agent.Id, "other.yml"), []models.RolePermission{update}, "insufficient permissions for the repository", true},
		{"other agent without deploy", body(app.Name.String(), other.Id, app.Path), readAll, "insufficient permissions to deploy", true},
		{"other path without deploy", body(app.Name.String(), agent.Id, "other.yml"), readAll, "insufficient permissions to deploy", true},
		{"new compose project without deploy", body("Renamed", agent.Id, app.Path), []models.RolePermission{update}, "insufficient permissions to deploy", true},
		{"new compose project with deploy", body("Renamed", agent.Id, app.Path), []models.RolePermission{update, deploy}, "", false},
		{"other agent with deploy", body(app.Name.String(), other.Id, app.Path), append(readAll, deploy), "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, w := makePermissionContext(t, http.MethodPut, "/api/v1/applications/"+app.Id, tt.body, tt.grants...)
			c.Params = gin.Params{{Key: "id", Value: app.Id}}
			UpdateApplicationHandler(c)

			// Without a reachable repository the update fails later, but not on permissions
			if tt.forbidden != (w.Code == http.StatusForbidden) || !bytes.Contains(w.Body.Bytes(), []byte(tt.want)) {
				t.Fatalf("expected forbidden=%v %q, got %d: %s", tt.forbidden, tt.want, w.Code, w.Body.String())
			}
		})
	}
}

func TestSyncRepositoryHandler_RequiresDeployOnItsApplications(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-sync")
	agent := seedTestAgent(t, "agent-sync")
	app := seedTestApplication(t, repo.Id, agent.Id, "Synced")
	sync := grantOn(models.ResourceRepository, models.ActionSync, repo.Id)

	for _, tt := range []struct {
		name      string
		grants    []models.RolePermission
		forbidden bool
	}{
		{"without deploy", []models.RolePermission{sync}, true},
		{"with deploy", []models.RolePermission{sync, grantOn(models.ResourceApplication, models.ActionDeploy, app.Id)}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, w := makePermissionContext(t, http.MethodPost, "/api/v1/repositories/"+repo.Id+"/sync", nil, tt.grants...)
			c.Params = gin.Params{{Key: "id", Value: repo.Id}}
			SyncRepositoryHandler(c)

			if tt.forbidden != (w.Code == http.StatusForbidden) {
				t.Fatalf("expected forbidden=%v, got %d: %s", tt.forbidden, w.Code, w.Body.String())
			}
		})
	}
}
