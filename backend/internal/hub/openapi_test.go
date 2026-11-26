package hub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/apitoken"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// undocumentedRoutes stay outside the spec because they are not JSON or follow
// a protocol of their own. They include streams, the log download, the agent
// socket, browser redirects, an image, MCP and OAuth for MCP clients.
var undocumentedRoutes = []string{
	"GET /events",
	"GET /applications/{id}/containers/{containerId}/logs/stream",
	"GET /applications/{id}/containers/{containerId}/logs/download",
	"GET /applications/{id}/containers/{containerId}/stats/stream",
	"GET /ws",
	"GET /auth/oidc/{id}/authorize",
	"GET /auth/oidc/{id}/callback",
	"GET /auth/profile-picture",
	// MCP clients speak JSON-RPC, the tools describe themselves.
	"GET /mcp",
	"POST /mcp",
	"DELETE /mcp",
	"OPTIONS /mcp",
	// OAuth for MCP clients, defined by RFC 7591 and RFC 6749. The token
	// endpoint takes a form.
	"POST /oauth/register",
	"OPTIONS /oauth/register",
	"POST /oauth/token",
	"OPTIONS /oauth/token",
}

var ginParam = regexp.MustCompile(`[:*](\w+)`)

func specOperations(spec *huma.OpenAPI) []string {
	var ops []string
	for path, item := range spec.Paths {
		for method, op := range map[string]*huma.Operation{
			http.MethodGet:    item.Get,
			http.MethodPost:   item.Post,
			http.MethodPut:    item.Put,
			http.MethodPatch:  item.Patch,
			http.MethodDelete: item.Delete,
		} {
			if op != nil {
				ops = append(ops, method+" "+path)
			}
		}
	}
	return ops
}

// The spec has to list every route under the path Gin serves it, because
// handlers.go and the operations name the paths separately.
func TestRegisterRoutes_OpenAPIMatchesTheRoutes(t *testing.T) {
	router := gin.New()
	if err := RegisterRoutes(router, Config{AppURL: "http://localhost:8080", DisableUI: true}); err != nil {
		t.Fatalf("failed to register routes: %v", err)
	}

	var routes []string
	for _, route := range router.Routes() {
		path, ok := strings.CutPrefix(route.Path, "/api/v1")
		if !ok {
			continue
		}
		route := route.Method + " " + ginParam.ReplaceAllString(path, "{$1}")
		if !slices.Contains(undocumentedRoutes, route) {
			routes = append(routes, route)
		}
	}
	documented := specOperations(restapi.OpenAPI())

	for _, route := range routes {
		if !slices.Contains(documented, route) {
			t.Errorf("%s is not in the OpenAPI spec", route)
		}
	}
	for _, op := range documented {
		if !slices.Contains(routes, op) {
			t.Errorf("%s is in the OpenAPI spec, but no route serves it", op)
		}
	}
}

func TestOpenAPI_OperationsAreDescribed(t *testing.T) {
	ids := map[string]bool{}
	for _, item := range restapi.OpenAPI().Paths {
		for _, op := range []*huma.Operation{item.Get, item.Post, item.Put, item.Patch, item.Delete} {
			if op == nil {
				continue
			}
			if op.Summary == "" || len(op.Tags) == 0 {
				t.Errorf("%s %s needs a summary and a tag", op.Method, op.Path)
			}
			// restapi.Public, Authenticated or SessionOnly
			if len(op.Security) == 0 {
				t.Errorf("%s %s needs a security requirement", op.Method, op.Path)
			}
			if ids[op.OperationID] {
				t.Errorf("operation ID %q is used twice", op.OperationID)
			}
			ids[op.OperationID] = true
		}
	}
}

// setupSpecTestDB connects to a migrated database with an admin and returns
// two API tokens of the admin: one with every scope and one without any.
func setupSpecTestDB(t *testing.T) (allScopes, noScopes string) {
	t.Helper()
	if err := crypto.Init("test-secret-that-is-long-enough-32chars"); err != nil {
		t.Fatalf("failed to init crypto: %v", err)
	}
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.Mkdir(filepath.Join(dir, "data"), 0o750); err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}
	if err := db.Connect(zerolog.Nop(), zerolog.InfoLevel, false); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB.DB(); err == nil {
			_ = sqlDB.Close()
		}
		db.DB = nil
	})

	ctx := t.Context()
	admin := models.User{Id: "spec-admin", Email: "admin@example.com", Name: "Admin", LocalAuthEnabled: true}
	if err := gorm.G[models.User](db.DB).Create(ctx, &admin); err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}
	role, err := gorm.G[models.Role](db.DB).Where("built_in = ?", models.RoleBuiltInAdmin).First(ctx)
	if err != nil {
		t.Fatalf("failed to load admin role: %v", err)
	}
	if err := gorm.G[models.UserRole](db.DB).Create(ctx, &models.UserRole{UserId: admin.Id, RoleId: role.Id, Source: models.UserRoleSourceManual}); err != nil {
		t.Fatalf("failed to assign admin role: %v", err)
	}

	createToken := func(name string, scopes []models.APITokenScope) string {
		generated := apitoken.Generate()
		token := models.APIToken{
			UserId:    admin.Id,
			Name:      name,
			TokenHash: generated.Hash,
			Prefix:    generated.Prefix,
			Scopes:    scopes,
			ExpiresAt: time.Now().Add(time.Hour),
		}
		if err := gorm.G[models.APIToken](db.DB).Create(ctx, &token); err != nil {
			t.Fatalf("failed to create api token: %v", err)
		}
		return generated.Plain
	}
	return createToken("all", models.AllAPITokenScopes()), createToken("none", []models.APITokenScope{})
}

var specPathParam = regexp.MustCompile(`\{\w+\}`)

// The security of each operation is declared apart from the middleware in
// handlers.go, so this sends every operation through the router: without
// credentials, and with API tokens of an admin with every scope and none.
func TestRegisterRoutes_SecurityMatchesTheMiddleware(t *testing.T) {
	token, noScopes := setupSpecTestDB(t)
	router := gin.New()
	if err := RegisterRoutes(router, Config{AppURL: "http://localhost:8080", DisableUI: true}); err != nil {
		t.Fatalf("failed to register routes: %v", err)
	}
	serve := func(op *huma.Operation, authorization string) (int, string) {
		path := "/api/v1" + specPathParam.ReplaceAllString(op.Path, "00000000-0000-0000-0000-000000000000")
		req := httptest.NewRequest(op.Method, path, nil)
		// Every request comes from another IP, so the rate limits don't kick in
		req.RemoteAddr = fmt.Sprintf("192.0.2.%d:1234", len(path)%250+1)
		if authorization != "" {
			req.Header.Set("Authorization", authorization)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		var body struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		return w.Code, body.Error
	}

	for _, item := range restapi.OpenAPI().Paths {
		for _, op := range []*huma.Operation{item.Get, item.Post, item.Put, item.Patch, item.Delete} {
			if op == nil {
				continue
			}
			name := op.Method + " " + op.Path
			public := reflect.DeepEqual(op.Security, restapi.Public)

			status, msg := serve(op, "")
			if public && msg == "missing authentication" {
				t.Errorf("%s is documented as public, but needs authentication", name)
			}
			if !public && (status != http.StatusUnauthorized || msg != "missing authentication") {
				t.Errorf("%s is documented as authenticated, but answered %d %q without credentials", name, status, msg)
			}
			if public {
				continue
			}

			_, msg = serve(op, "Bearer "+token)
			sessionOnly := reflect.DeepEqual(op.Security, restapi.SessionOnly)
			refused := msg == "this route can't be used with an api token"
			if sessionOnly && !refused {
				t.Errorf("%s is documented as session only, but took an API token", name)
			}
			if !sessionOnly && refused {
				t.Errorf("%s is documented to take API tokens, but refused one", name)
			}
			if sessionOnly {
				continue
			}

			// RequirePermission and RequireAdmin refuse a token without scopes
			status, msg = serve(op, "Bearer "+noScopes)
			if _, documented := op.Responses["403"]; status == http.StatusForbidden && !documented {
				t.Errorf("%s answers 403 %q, but doesn't document it", name, msg)
			}
		}
	}
}
