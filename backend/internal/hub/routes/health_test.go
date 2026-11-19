package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestHealthHandler(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)

	HealthHandler(c)

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

func TestHealthHandler_SpecRequiresStatus(t *testing.T) {
	spec := restapi.OpenAPI()
	ref := spec.Paths["/health"].Get.Responses["200"].Content["application/json"].Schema.Ref
	schema := spec.Components.Schemas.SchemaFromRef(ref)
	if !slices.Contains(schema.Required, "status") {
		t.Errorf("expected status to be required, got %v", schema.Required)
	}
}
