package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// serveRestAPI registers routes on a Huma API like the hub does and sends one request.
func serveRestAPI(t *testing.T, register func(huma.API), method, path string) *httptest.ResponseRecorder {
	t.Helper()
	router := gin.New()
	register(restapi.New(router, router.Group("/api/v1")))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

func TestHealth(t *testing.T) {
	w := serveRestAPI(t, RegisterHealth, http.MethodGet, "/api/v1/health")

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if body["status"] != "ok" || len(body) != 1 {
		t.Errorf(`expected {"status": "ok"}, got %v`, body)
	}
}
