package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
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

func TestRegisterRoutes_EventsEndpointDisabledInDemoMode(t *testing.T) {
	for _, demo := range []bool{false, true} {
		router := gin.New()
		if err := RegisterRoutes(router, Config{DisableUI: true, Demo: demo}); err != nil {
			t.Fatalf("Failed to register routes: %v", err)
		}

		registered := false
		for _, route := range router.Routes() {
			if route.Method == http.MethodGet && route.Path == "/api/v1/events" {
				registered = true
			}
		}
		if registered == demo {
			t.Errorf("demo=%v: events route registered = %v", demo, registered)
		}
	}
}

func TestRegisterRoutes_OIDCEndpointsRateLimited(t *testing.T) {
	testDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	if err := testDB.AutoMigrate(&models.OIDCProvider{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
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

	router := gin.New()
	if err := RegisterRoutes(router, Config{DisableUI: true}); err != nil {
		t.Fatalf("Failed to register routes: %v", err)
	}

	tests := []struct {
		name       string
		path       string
		remoteAddr string
		wantStatus int
	}{
		{"authorize", "/api/v1/auth/oidc/unknown/authorize", "192.0.2.10:1234", http.StatusNotFound},
		{"callback", "/api/v1/auth/oidc/unknown/callback?error=access_denied", "192.0.2.11:1234", http.StatusFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			do := func() int {
				w := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodGet, tt.path, nil)
				req.RemoteAddr = tt.remoteAddr
				router.ServeHTTP(w, req)
				return w.Code
			}

			// Burst of 5 is allowed
			for i := range 5 {
				if got := do(); got != tt.wantStatus {
					t.Fatalf("request %d: expected %d, got %d", i+1, tt.wantStatus, got)
				}
			}

			if got := do(); got != http.StatusTooManyRequests {
				t.Errorf("expected 429 after burst, got %d", got)
			}
		})
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
