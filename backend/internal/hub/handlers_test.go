package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestRegisterRoutes_HealthEndpoint(t *testing.T) {
	router := gin.New()
	cfg := Config{DisableUI: true}
	err := RegisterRoutes(router, cfg)
	if err != nil {
		t.Fatalf("Failed to register routes: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("expected status %q, got %q", "ok", body["status"])
	}
}

func TestRegisterRoutes_DisableUI_NoStaticFiles(t *testing.T) {
	router := gin.New()
	cfg := Config{DisableUI: true}
	err := RegisterRoutes(router, cfg)
	if err != nil {
		t.Fatalf("Failed to register routes: %v", err)
	}

	// In DisableUI mode, requesting a non-API path should 404 (no static serving)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 in DisableUI mode for non-API path, got %d", w.Code)
	}
}
