package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/apitoken"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// The scopes of an API token decide which tools the MCP endpoint offers, even
// when the user's role allows more.
func TestMCPEndpoint_ToolsFollowTokenScopes(t *testing.T) {
	testDB := setupMCPTestDB(t)

	user := models.User{Id: "user-1", Name: "CI", Email: "ci@example.com"}
	if err := gorm.G[models.User](testDB).Create(t.Context(), &user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	// The user role may deploy every application.
	if err := rbac.AssignBuiltInRole(t.Context(), testDB, user.Id, models.RoleBuiltInUser); err != nil {
		t.Fatalf("failed to assign role: %v", err)
	}
	generated := apitoken.Generate()
	token := models.APIToken{
		UserId:    user.Id,
		Name:      "ci",
		TokenHash: generated.Hash,
		Prefix:    generated.Prefix,
		Scopes:    []models.APITokenScope{models.NewAPITokenScope(models.ResourceApplication, models.ActionRead)},
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := gorm.G[models.APIToken](testDB).Create(t.Context(), &token); err != nil {
		t.Fatalf("failed to create api token: %v", err)
	}

	router := gin.New()
	if err := RegisterRoutes(router, Config{AppURL: "http://localhost:8080", DisableUI: true}); err != nil {
		t.Fatalf("Failed to register routes: %v", err)
	}

	w := listMCPTools(router, generated.Plain)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if names, want := mcpToolNames(t, w), []string{"get_application", "list_applications"}; !slices.Equal(names, want) {
		t.Errorf("tools = %v, want %v", names, want)
	}
}

func setupMCPTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	testDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	if err := testDB.AutoMigrate(&models.User{}, &models.APIToken{}, &models.AuditLog{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	if err := rbac.MigrateForTest(testDB); err != nil {
		t.Fatalf("failed to migrate roles: %v", err)
	}
	sqlDB, err := testDB.DB()
	if err != nil {
		t.Fatalf("failed to get sql db: %v", err)
	}
	db.DB = testDB
	t.Cleanup(func() {
		_ = sqlDB.Close()
		db.DB = nil
	})
	return testDB
}

func listMCPTools(router *gin.Engine, token string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	router.ServeHTTP(w, req)
	return w
}

func mcpToolNames(t *testing.T, w *httptest.ResponseRecorder) []string {
	t.Helper()
	var resp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON-RPC response %q: %v", w.Body.String(), err)
	}
	var names []string
	for _, tool := range resp.Result.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	return names
}
