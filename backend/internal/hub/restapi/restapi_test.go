package restapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

type createInput struct {
	Body struct {
		Name     string `json:"name" minLength:"1"`
		Password string `json:"password" minLength:"8"`
		Note     string `json:"note"`
	}
}

type createOutput struct {
	Body struct {
		Name string `json:"name"`
		Note string `json:"note"`
	}
}

func newTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	router := gin.New()
	api := New(router, router.Group("/api/v1"))
	huma.Register(api, huma.Operation{
		OperationID: "create-thing",
		Method:      http.MethodPost,
		Path:        "/things",
	}, func(_ context.Context, input *createInput) (*createOutput, error) {
		out := &createOutput{}
		out.Body.Name = input.Body.Name
		out.Body.Note = input.Body.Note
		return out, nil
	})
	huma.Get(api, "/things/{id}", func(_ context.Context, input *struct {
		ID string `path:"id"`
	}) (*createOutput, error) {
		return nil, huma.Error404NotFound("thing not found")
	})
	return router
}

func serve(router *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	router.ServeHTTP(w, req)
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON %q: %v", w.Body.String(), err)
	}
	return body
}

func TestNew_ErrorsUseTheGinFormat(t *testing.T) {
	w := serve(newTestRouter(t), http.MethodGet, "/api/v1/things/1", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("expected application/json, got %q", ct)
	}
	body := decode(t, w)
	if body["error"] != "thing not found" || len(body) != 1 {
		t.Errorf(`expected only {"error": "thing not found"}, got %v`, body)
	}
}

func TestNew_ValidationErrorsAre400WithoutValues(t *testing.T) {
	w := serve(newTestRouter(t), http.MethodPost, "/api/v1/things", `{"name": "", "password": "secret"}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	msg, _ := decode(t, w)["error"].(string)
	if !strings.Contains(msg, "body.name") || !strings.Contains(msg, "body.password") {
		t.Errorf("expected both invalid fields in %q", msg)
	}
	if strings.Contains(msg, "secret") {
		t.Errorf("expected the value to be left out of %q", msg)
	}
}

func TestNew_MalformedBodyIs400(t *testing.T) {
	w := serve(newTestRouter(t), http.MethodPost, "/api/v1/things", `{"name":`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if _, ok := decode(t, w)["error"].(string); !ok {
		t.Errorf("expected an error message, got %s", w.Body.String())
	}
}

func TestNew_AcceptsUnknownAndMissingFields(t *testing.T) {
	// "note" is missing and "extra" is unknown, Gin's binding accepts both
	w := serve(newTestRouter(t), http.MethodPost, "/api/v1/things", `{"name": "a", "password": "12345678", "extra": true}`)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := decode(t, w)
	if _, ok := body["$schema"]; ok {
		t.Errorf("expected no $schema field, got %v", body)
	}
	if link := w.Header().Get("Link"); link != "" {
		t.Errorf("expected no Link header, got %q", link)
	}
}

func TestNew_DoesNotServeTheSpecOrDocs(t *testing.T) {
	router := newTestRouter(t)
	for _, path := range []string{"/api/v1/openapi.json", "/api/v1/openapi.yaml", "/api/v1/docs", "/api/v1/schemas/Error.json"} {
		if w := serve(router, http.MethodGet, path, ""); w.Code != http.StatusNotFound {
			t.Errorf("%s: expected 404, got %d", path, w.Code)
		}
	}
}

func TestNew_SpecDescribesTheRoutes(t *testing.T) {
	api := New(gin.New(), gin.New().Group("/api/v1"))
	huma.Get(api, "/ping", func(_ context.Context, _ *struct{}) (*struct{}, error) { return nil, nil })

	spec := api.OpenAPI()
	if spec.Servers[0].URL != "/api/v1" {
		t.Errorf("expected server /api/v1, got %q", spec.Servers[0].URL)
	}
	if spec.Paths["/ping"] == nil {
		t.Errorf("expected /ping in the spec, got %v", spec.Paths)
	}
}

func TestNew_SpecDocumentsValidationErrorsAs400(t *testing.T) {
	api := New(gin.New(), gin.New().Group("/api/v1"))
	huma.Register(api, huma.Operation{
		OperationID: "create-thing",
		Method:      http.MethodPost,
		Path:        "/things",
		Errors:      []int{http.StatusNotFound},
	}, func(_ context.Context, _ *createInput) (*createOutput, error) { return nil, nil })

	responses := api.OpenAPI().Paths["/things"].Post.Responses
	if _, ok := responses["422"]; ok {
		t.Error("expected no 422 response, newError answers with 400")
	}
	if r, ok := responses["400"]; !ok || r.Description != "Bad Request" {
		t.Errorf("expected a 400 Bad Request response, got %+v", r)
	}
	if _, ok := responses["404"]; !ok {
		t.Error("expected the declared 404 response to stay")
	}
}

func TestNew_SpecMarksRequiredResponseFields(t *testing.T) {
	api := New(gin.New(), gin.New().Group("/api/v1"))
	huma.Get(api, "/ping", func(_ context.Context, _ *struct{}) (*struct {
		Body struct {
			Status string `json:"status" required:"true"`
			Note   string `json:"note"`
		}
	}, error) {
		return nil, nil
	})

	schema := api.OpenAPI().Paths["/ping"].Get.Responses["200"].Content["application/json"].Schema
	if schema.Ref != "" {
		schema = api.OpenAPI().Components.Schemas.SchemaFromRef(schema.Ref)
	}
	if !slices.Equal(schema.Required, []string{"status"}) {
		t.Errorf("expected only status to be required, got %v", schema.Required)
	}
}
