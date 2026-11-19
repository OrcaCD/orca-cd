package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const testComposeFile = "services:\n  web:\n    image: nginx:1.27\n"

func init() {
	gin.SetMode(gin.TestMode)
}

type stubDeployer struct {
	mu       sync.Mutex
	deployed []string
	err      error
}

func (d *stubDeployer) TriggerApplicationDeploy(_ context.Context, app *models.Application, _, _ string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deployed = append(d.deployed, app.Id)
	return d.err
}

func (d *stubDeployer) TriggerRollback(context.Context, *models.Application, *models.ApplicationDeployment, string) error {
	return nil
}

func setupTestDB(t *testing.T) {
	t.Helper()

	testDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	if err := testDB.AutoMigrate(&models.Agent{}, &models.Repository{}, &models.Application{}, &models.ApplicationEvent{}, &models.ApplicationDeployment{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	sqlDB, err := testDB.DB()
	if err != nil {
		t.Fatalf("failed to get sql db: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
		db.DB = nil
	})
	db.DB = testDB

	if err := crypto.Init("test-secret-that-is-long-enough-32chars"); err != nil {
		t.Fatalf("failed to init crypto: %v", err)
	}
}

func seedApplication(t *testing.T, name string) models.Application {
	t.Helper()

	agent := models.Agent{Name: crypto.EncryptedString("agent-" + name), KeyId: crypto.EncryptedString("key-" + name), Status: models.AgentStatusOnline}
	if err := db.DB.Create(&agent).Error; err != nil {
		t.Fatalf("failed to seed agent: %v", err)
	}
	repo := models.Repository{
		Name:       "owner/" + name,
		Url:        "https://github.com/owner/" + name,
		Provider:   models.GitHub,
		AuthMethod: models.AuthMethodNone,
		SyncType:   models.SyncTypeManual,
		SyncStatus: models.SyncStatusUnknown,
		CreatedBy:  "user-1",
	}
	if err := db.DB.Create(&repo).Error; err != nil {
		t.Fatalf("failed to seed repository: %v", err)
	}
	app := models.Application{
		Name:                crypto.EncryptedString(name),
		RepositoryId:        repo.Id,
		AgentId:             agent.Id,
		SyncStatus:          models.OutOfSync,
		HealthStatus:        models.Healthy,
		Branch:              "main",
		Commit:              "new-commit",
		CommitMessage:       "update nginx",
		Path:                "compose.yml",
		ComposeFile:         crypto.EncryptedString(testComposeFile),
		PreviousComposeFile: crypto.EncryptedString(""),
		Warnings:            "[]",
	}
	if err := db.DB.Create(&app).Error; err != nil {
		t.Fatalf("failed to seed application: %v", err)
	}
	return app
}

// connect starts the endpoint for a user with the given permissions and
// returns a connected client session.
func connect(t *testing.T, user *models.User, perms *rbac.Permissions) *mcp.ClientSession {
	t.Helper()

	router := gin.New()
	router.POST("/mcp", func(c *gin.Context) {
		auth.SetUser(c, user)
		rbac.Set(c, perms)
		c.Next()
	}, Handler())
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func testUser() *models.User {
	return &models.User{Id: "user-1", Name: "Test User"}
}

func grant(resource models.PermissionResource, action models.PermissionAction, id string) models.RolePermission {
	g := models.RolePermission{Resource: resource, Action: action}
	if id != "" {
		g.ResourceId = &id
	}
	return g
}

// callTool calls a tool and decodes its structured result into out. It
// returns the error text of a failed call.
func callTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any, out any) string {
	t.Helper()

	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if res.IsError {
		if len(res.Content) == 0 {
			t.Fatalf("call %s: error result without content", name)
		}
		return res.Content[0].(*mcp.TextContent).Text
	}
	if out != nil {
		raw, err := json.Marshal(res.StructuredContent)
		if err != nil {
			t.Fatalf("marshal %s result: %v", name, err)
		}
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("decode %s result: %v", name, err)
		}
	}
	return ""
}

func toolNames(t *testing.T, session *mcp.ClientSession) []string {
	t.Helper()

	res, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	names := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	return names
}

func TestToolsDependOnPermissions(t *testing.T) {
	setupTestDB(t)

	tests := []struct {
		name  string
		perms *rbac.Permissions
		want  []string
	}{
		{"admin", rbac.Admin(), []string{"deploy_application", "get_application", "list_agents", "list_applications", "preview_deploy"}},
		{"read applications", rbac.New([]models.RolePermission{grant(models.ResourceApplication, models.ActionRead, "")}), []string{"get_application", "list_applications"}},
		{"deploy one application", rbac.New([]models.RolePermission{grant(models.ResourceApplication, models.ActionDeploy, "app-1")}), []string{"deploy_application", "get_application", "list_applications", "preview_deploy"}},
		{"agents only", rbac.New([]models.RolePermission{grant(models.ResourceAgent, models.ActionRead, "")}), []string{"list_agents"}},
		{"nothing", rbac.New(nil), []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := connect(t, testUser(), tt.perms)
			if got := toolNames(t, session); !slices.Equal(got, tt.want) {
				t.Errorf("tools = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHandlerRejectsRequestsWithoutUser(t *testing.T) {
	router := gin.New()
	router.POST("/mcp", Handler())

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestHandlerOnlyAcceptsPost(t *testing.T) {
	router := gin.New()
	router.Match([]string{http.MethodGet, http.MethodDelete}, "/mcp", Handler())

	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(method, "/mcp", nil))
		if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != http.MethodPost {
			t.Errorf("%s: expected 405 with Allow: POST, got %d %q", method, w.Code, w.Header().Get("Allow"))
		}
	}
}

func TestHandlerRejectsBatchesAndLargeBodies(t *testing.T) {
	router := gin.New()
	router.POST("/mcp", func(c *gin.Context) {
		auth.SetUser(c, testUser())
		rbac.Set(c, rbac.Admin())
	}, Handler())

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{"batch", ` [{"jsonrpc":"2.0","id":1,"method":"tools/list"},{"jsonrpc":"2.0","id":2,"method":"tools/list"}]`, http.StatusBadRequest},
		{"too large", `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"pad":"` + strings.Repeat("a", maxRequestBytes) + `"}}`, http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("expected %d, got %d: %s", tt.wantStatus, w.Code, w.Body.String())
			}
		})
	}
}
