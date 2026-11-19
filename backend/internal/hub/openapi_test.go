package hub

import (
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"
)

// undocumentedRoutes stay plain Gin handlers outside the spec, as they aren't
// JSON: the event stream, the agent socket, browser redirects and an image.
var undocumentedRoutes = []string{
	"GET /events",
	"GET /ws",
	"GET /auth/oidc/{id}/authorize",
	"GET /auth/oidc/{id}/callback",
	"GET /auth/profile-picture",
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
			if ids[op.OperationID] {
				t.Errorf("operation ID %q is used twice", op.OperationID)
			}
			ids[op.OperationID] = true
		}
	}
}
