package routes

import (
	"bytes"
	"encoding/json"
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

func setupRolesTestDB(t *testing.T) {
	t.Helper()
	setupTestDBWithApplications(t)
	if err := db.DB.AutoMigrate(&models.AuditLog{}, &models.Notification{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
}

func newRolesRouter() *gin.Engine {
	router := gin.New()
	router.GET("/api/v1/admin/roles", AdminListRolesHandler)
	router.POST("/api/v1/admin/roles", AdminCreateRoleHandler)
	router.GET("/api/v1/admin/roles/:id", AdminGetRoleHandler)
	router.PUT("/api/v1/admin/roles/:id", AdminUpdateRoleHandler)
	router.DELETE("/api/v1/admin/roles/:id", AdminDeleteRoleHandler)
	return router
}

func serveRoleRequest(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
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
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	newRolesRouter().ServeHTTP(w, req)
	return w
}

func decodeRole(t *testing.T, w *httptest.ResponseRecorder) roleResponse {
	t.Helper()
	var role roleResponse
	if err := json.Unmarshal(w.Body.Bytes(), &role); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	return role
}

func TestAdminListRolesHandler_BuiltInRolesFirst(t *testing.T) {
	setupRolesTestDB(t)
	admin := createTestUser(t, "Admin", "admin@example.com", rbac.AdminRoleID, "password123")

	w := serveRoleRequest(t, http.MethodPost, "/api/v1/admin/roles", map[string]any{"name": "Auditors"})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	w = serveRoleRequest(t, http.MethodGet, "/api/v1/admin/roles", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var roles []roleResponse
	if err := json.Unmarshal(w.Body.Bytes(), &roles); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	names := make([]string, 0, len(roles))
	for _, role := range roles {
		names = append(names, role.Name)
	}
	if !slices.Equal(names, []string{"Admin", "User", "Auditors"}) {
		t.Fatalf("unexpected order %v", names)
	}
	if len(roles[0].Members) != 1 || roles[0].Members[0].Id != admin.Id {
		t.Fatalf("expected the admin to be a member of the admin role, got %+v", roles[0].Members)
	}
	if len(roles[1].Permissions) == 0 {
		t.Fatal("expected the user role to have permissions")
	}
}

func TestAdminCreateRoleHandler_Success(t *testing.T) {
	setupRolesTestDB(t)
	createTestUser(t, "Admin", "admin@example.com", rbac.AdminRoleID, "password123")
	member := createTestUser(t, "Member", "member@example.com", "", "password123")
	app := seedTestApplication(t, seedTestRepository(t, "https://github.com/owner/repo").Id, seedTestAgent(t, "agent").Id, "App")

	w := serveRoleRequest(t, http.MethodPost, "/api/v1/admin/roles", map[string]any{
		"name":        "  Deployers ",
		"description": "Deploy the shop",
		"permissions": []map[string]any{
			{"resource": "application", "action": "read"},
			{"resource": "application", "action": "deploy", "resourceId": app.Id},
			{"resource": "application", "action": "deploy", "resourceId": app.Id},
		},
		"userIds": []string{member.Id, member.Id},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	role := decodeRole(t, w)
	if role.Name != "Deployers" || role.Description != "Deploy the shop" || role.BuiltIn != nil {
		t.Fatalf("unexpected role %+v", role)
	}
	if len(role.Permissions) != 2 {
		t.Fatalf("expected duplicate permissions to be dropped, got %+v", role.Permissions)
	}
	if len(role.Members) != 1 || role.Members[0].Id != member.Id {
		t.Fatalf("expected member, got %+v", role.Members)
	}

	perms, err := rbac.Load(t.Context(), db.DB, member.Id)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !perms.Can(models.ResourceApplication, models.ActionDeploy, app.Id) {
		t.Fatal("expected the member to be allowed to deploy the application")
	}

	count, err := gorm.G[models.AuditLog](db.DB).Where("event_type = ? AND target_type = ? AND target_id = ?", "created", "role", role.Id).Count(t.Context(), "*")
	if err != nil || count != 1 {
		t.Fatalf("expected an audit log entry, count=%d err=%v", count, err)
	}
}

func TestAdminCreateRoleHandler_Validation(t *testing.T) {
	setupRolesTestDB(t)

	tests := []struct {
		name string
		body map[string]any
		code int
	}{
		{"missing name", map[string]any{"name": "  "}, http.StatusBadRequest},
		{"unknown action", map[string]any{"name": "R", "permissions": []map[string]any{{"resource": "agent", "action": "deploy"}}}, http.StatusBadRequest},
		{"unknown resource type", map[string]any{"name": "R", "permissions": []map[string]any{{"resource": "user", "action": "read"}}}, http.StatusBadRequest},
		{"scoped create", map[string]any{"name": "R", "permissions": []map[string]any{{"resource": "agent", "action": "create", "resourceId": "x"}}}, http.StatusBadRequest},
		{"missing resource", map[string]any{"name": "R", "permissions": []map[string]any{{"resource": "agent", "action": "read", "resourceId": "missing"}}}, http.StatusBadRequest},
		{"missing user", map[string]any{"name": "R", "userIds": []string{"missing"}}, http.StatusBadRequest},
		{"name of built-in role", map[string]any{"name": "admin"}, http.StatusConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := serveRoleRequest(t, http.MethodPost, "/api/v1/admin/roles", tt.body)
			if w.Code != tt.code {
				t.Fatalf("expected %d, got %d: %s", tt.code, w.Code, w.Body.String())
			}
		})
	}
}

func TestAdminUpdateRoleHandler_BuiltInRoles(t *testing.T) {
	setupRolesTestDB(t)
	admin := createTestUser(t, "Admin", "admin@example.com", rbac.AdminRoleID, "password123")
	other := createTestUser(t, "Other", "other@example.com", "", "password123")

	// Only the members of the admin role can change.
	w := serveRoleRequest(t, http.MethodPut, "/api/v1/admin/roles/"+rbac.AdminRoleID, map[string]any{
		"name":        "Renamed",
		"permissions": []map[string]any{{"resource": "agent", "action": "read"}},
		"userIds":     []string{admin.Id, other.Id},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	role := decodeRole(t, w)
	if role.Name != "Admin" || len(role.Permissions) != 0 || len(role.Members) != 2 {
		t.Fatalf("unexpected admin role %+v", role)
	}

	// The user role keeps its name but its permissions change.
	w = serveRoleRequest(t, http.MethodPut, "/api/v1/admin/roles/"+rbac.UserRoleID, map[string]any{
		"name":        "Renamed",
		"permissions": []map[string]any{{"resource": "agent", "action": "read"}},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	role = decodeRole(t, w)
	if role.Name != "User" || len(role.Permissions) != 1 {
		t.Fatalf("unexpected user role %+v", role)
	}
}

func TestAdminUpdateRoleHandler_RejectsRemovingLastAdmin(t *testing.T) {
	setupRolesTestDB(t)
	admin := createTestUser(t, "Admin", "admin@example.com", rbac.AdminRoleID, "password123")

	w := serveRoleRequest(t, http.MethodPut, "/api/v1/admin/roles/"+rbac.AdminRoleID, map[string]any{"name": "Admin", "userIds": []string{}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	isAdmin, err := rbac.IsAdmin(t.Context(), db.DB, admin.Id)
	if err != nil || !isAdmin {
		t.Fatalf("expected the admin to keep the role, err=%v", err)
	}
}

func TestAdminUpdateRoleHandler_NotFound(t *testing.T) {
	setupRolesTestDB(t)

	w := serveRoleRequest(t, http.MethodPut, "/api/v1/admin/roles/missing", map[string]any{"name": "R"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminDeleteRoleHandler(t *testing.T) {
	setupRolesTestDB(t)
	createTestUser(t, "Admin", "admin@example.com", rbac.AdminRoleID, "password123")
	member := createTestUser(t, "Member", "member@example.com", "", "password123")

	w := serveRoleRequest(t, http.MethodPost, "/api/v1/admin/roles", map[string]any{
		"name":        "Temporary",
		"permissions": []map[string]any{{"resource": "agent", "action": "read"}},
		"userIds":     []string{member.Id},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	role := decodeRole(t, w)

	for _, id := range []string{rbac.AdminRoleID, rbac.UserRoleID} {
		if w := serveRoleRequest(t, http.MethodDelete, "/api/v1/admin/roles/"+id, nil); w.Code != http.StatusBadRequest {
			t.Fatalf("expected deleting a built-in role to fail, got %d: %s", w.Code, w.Body.String())
		}
	}

	if w := serveRoleRequest(t, http.MethodDelete, "/api/v1/admin/roles/"+role.Id, nil); w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w := serveRoleRequest(t, http.MethodGet, "/api/v1/admin/roles/"+role.Id, nil); w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", w.Code)
	}

	remaining, err := gorm.G[models.UserRole](db.DB).Where("role_id = ?", role.Id).Count(t.Context(), "*")
	if err != nil || remaining != 0 {
		t.Fatalf("expected assignments to be removed, count=%d err=%v", remaining, err)
	}
	grants, err := gorm.G[models.RolePermission](db.DB).Where("role_id = ?", role.Id).Count(t.Context(), "*")
	if err != nil || grants != 0 {
		t.Fatalf("expected permissions to be removed, count=%d err=%v", grants, err)
	}
}
