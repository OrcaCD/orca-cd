package restapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
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

type userKey struct{}

var (
	createThing = Handler(huma.Operation{
		OperationID:   "create-thing",
		Method:        http.MethodPost,
		Path:          "/things",
		DefaultStatus: http.StatusCreated,
		Errors:        []int{http.StatusNotFound},
		Security:      Authenticated,
	}, func(_ context.Context, input *createInput) (*createOutput, error) {
		out := &createOutput{}
		out.Body.Name = input.Body.Name
		out.Body.Note = input.Body.Note
		return out, nil
	})

	getThing = Handler(huma.Operation{
		OperationID: "get-thing",
		Method:      http.MethodGet,
		Path:        "/things/{id}",
	}, func(ctx context.Context, input *struct {
		ID string `path:"id"`
	}) (*createOutput, error) {
		// The Gin context is reachable, e.g. for the user a middleware set
		if user, _ := GinContext(ctx).Get("user"); user != "alice" {
			return nil, huma.Error401Unauthorized("missing authentication")
		}
		if input.ID != "1" {
			return nil, huma.Error404NotFound("thing not found")
		}
		return &createOutput{}, nil
	})

	deleteThing = Handler(huma.Operation{
		OperationID: "delete-thing",
		Method:      http.MethodDelete,
		Path:        "/things/{id}",
	}, func(_ context.Context, _ *struct {
		ID string `path:"id"`
	}) (*MessageOutput, error) {
		return Message("thing deleted"), nil
	})

	pingThings = Handler(huma.Operation{
		OperationID: "ping-things",
		Method:      http.MethodGet,
		Path:        "/things/ping",
	}, func(_ context.Context, _ *struct{}) (*struct {
		Body struct {
			Status string `json:"status" required:"true"`
			Note   string `json:"note"`
		}
	}, error) {
		return nil, nil
	})
)

func newTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	router := gin.New()
	group := router.Group("/api/v1", func(c *gin.Context) { c.Set("user", "alice") })
	group.POST("/things", createThing)
	group.GET("/things/:id", getThing)
	group.DELETE("/things/:id", deleteThing)
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

func TestHandler_RunsBehindGinMiddleware(t *testing.T) {
	w := serve(newTestRouter(t), http.MethodGet, "/api/v1/things/1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Without the middleware that sets the user
	router := gin.New()
	router.GET("/api/v1/things/:id", getThing)
	if w := serve(router, http.MethodGet, "/api/v1/things/1", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandler_WorksWithAGinTestContext(t *testing.T) {
	// Like the route tests, which call handlers without a router
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/things/1", nil)
	c.Params = gin.Params{{Key: "id", Value: "1"}}
	c.Set("user", "alice")

	getThing(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandler_UsesTheDefaultStatusAndMessages(t *testing.T) {
	router := newTestRouter(t)
	if w := serve(router, http.MethodPost, "/api/v1/things", `{"name": "a", "password": "12345678"}`); w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	w := serve(router, http.MethodDelete, "/api/v1/things/1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if body := decode(t, w); body["message"] != "thing deleted" || len(body) != 1 {
		t.Errorf(`expected only {"message": "thing deleted"}, got %v`, body)
	}
}

func TestHandler_ErrorsUseTheGinFormat(t *testing.T) {
	w := serve(newTestRouter(t), http.MethodGet, "/api/v1/things/2", "")

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

func TestHandler_ValidationErrorsAre400WithoutValues(t *testing.T) {
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

func TestHandler_MalformedOrMissingBodyIs400(t *testing.T) {
	router := newTestRouter(t)
	for _, body := range []string{`{"name":`, ""} {
		w := serve(router, http.MethodPost, "/api/v1/things", body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%q: expected 400, got %d: %s", body, w.Code, w.Body.String())
		}
		if _, ok := decode(t, w)["error"].(string); !ok {
			t.Errorf("%q: expected an error message, got %s", body, w.Body.String())
		}
	}
}

func TestHandler_AcceptsUnknownAndMissingFields(t *testing.T) {
	// "note" is missing and "extra" is unknown, Gin's binding accepts both
	w := serve(newTestRouter(t), http.MethodPost, "/api/v1/things", `{"name": "a", "password": "12345678", "extra": true}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	body := decode(t, w)
	if _, ok := body["$schema"]; ok {
		t.Errorf("expected no $schema field, got %v", body)
	}
	if link := w.Header().Get("Link"); link != "" {
		t.Errorf("expected no Link header, got %q", link)
	}
}

func TestOpenAPI_DescribesTheOperations(t *testing.T) {
	spec := OpenAPI()
	if spec.Servers[0].URL != "/api/v1" {
		t.Errorf("expected server /api/v1, got %q", spec.Servers[0].URL)
	}
	op := spec.Paths["/things"].Post
	if op == nil {
		t.Fatalf("expected POST /things in the spec, got %v", spec.Paths)
	}
	if !reflect.DeepEqual(op.Security, Authenticated) {
		t.Errorf("expected the operation to take a token or the session, got %v", op.Security)
	}
	for _, scheme := range []string{apiTokenScheme, sessionScheme} {
		if spec.Components.SecuritySchemes[scheme] == nil {
			t.Errorf("expected the %s security scheme", scheme)
		}
	}
	for _, path := range []string{"/openapi.json", "/docs", "/schemas/Error.json"} {
		if w := serve(newTestRouter(t), http.MethodGet, "/api/v1"+path, ""); w.Code != http.StatusNotFound {
			t.Errorf("%s: expected 404, got %d", path, w.Code)
		}
	}
}

func TestOpenAPI_DocumentsValidationErrorsAs400(t *testing.T) {
	responses := OpenAPI().Paths["/things"].Post.Responses
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

func TestOpenAPI_MarksRequiredResponseFields(t *testing.T) {
	_ = pingThings
	schema := OpenAPI().Paths["/things/ping"].Get.Responses["200"].Content["application/json"].Schema
	if schema.Ref != "" {
		schema = OpenAPI().Components.Schemas.SchemaFromRef(schema.Ref)
	}
	if !slices.Equal(schema.Required, []string{"status"}) {
		t.Errorf("expected only status to be required, got %v", schema.Required)
	}
}
