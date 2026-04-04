package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestListProvidersHandler_NoProviders(t *testing.T) {
	setupTestDB(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/providers", nil)

	LocalAuthDisabled = false
	ListProvidersHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body providersResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(body.Providers) != 0 {
		t.Errorf("expected 0 providers, got %d", len(body.Providers))
	}
	if !body.LocalAuthEnabled {
		t.Error("expected localAuthEnabled=true")
	}
}

func TestListProvidersHandler_WithProviders(t *testing.T) {
	setupTestDB(t)

	if err := gorm.G[models.OIDCProvider](db.DB).Create(t.Context(), &models.OIDCProvider{
		Name:         "Enabled IDP",
		IssuerURL:    "https://idp.example.com",
		ClientID:     "client-1",
		ClientSecret: crypto.EncryptedString("secret"),
		Enabled:      true,
	}); err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	disabled := models.OIDCProvider{
		Name:         "Disabled IDP",
		IssuerURL:    "https://disabled.example.com",
		ClientID:     "client-2",
		ClientSecret: crypto.EncryptedString("secret"),
		Enabled:      true,
	}
	if err := gorm.G[models.OIDCProvider](db.DB).Create(t.Context(), &disabled); err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	// GORM skips zero-value bool on Create, so update explicitly.
	db.DB.Model(&models.OIDCProvider{}).Where("id = ?", disabled.Id).Update("enabled", false)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/providers", nil)

	LocalAuthDisabled = false
	ListProvidersHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body providersResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(body.Providers) != 1 {
		t.Fatalf("expected 1 enabled provider, got %d", len(body.Providers))
	}
	if body.Providers[0].Name != "Enabled IDP" {
		t.Errorf("expected provider name %q, got %q", "Enabled IDP", body.Providers[0].Name)
	}
}

func TestListProvidersHandler_LocalAuthDisabled(t *testing.T) {
	setupTestDB(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/providers", nil)

	LocalAuthDisabled = true
	t.Cleanup(func() { LocalAuthDisabled = false })
	ListProvidersHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body providersResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.LocalAuthEnabled {
		t.Error("expected localAuthEnabled=false when LocalAuthDisabled=true")
	}
}

func TestOIDCCallbackHandler_ErrorParam(t *testing.T) {
	setupTestDB(t)

	router := gin.New()
	router.GET("/api/v1/auth/oidc/:id/callback", OIDCCallbackHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/prov-1/callback?error=access_denied", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "/login?error=access_denied" {
		t.Errorf("unexpected redirect: %s", loc)
	}
}

func TestOIDCCallbackHandler_MissingCodeOrState(t *testing.T) {
	setupTestDB(t)

	router := gin.New()
	router.GET("/api/v1/auth/oidc/:id/callback", OIDCCallbackHandler)

	tests := []struct {
		name  string
		query string
	}{
		{"missing both", ""},
		{"missing state", "code=abc"},
		{"missing code", "state=xyz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			url := "/api/v1/auth/oidc/prov-1/callback"
			if tt.query != "" {
				url += "?" + tt.query
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			router.ServeHTTP(w, req)

			if w.Code != http.StatusFound {
				t.Fatalf("expected 302, got %d", w.Code)
			}
			loc := w.Header().Get("Location")
			if loc != "/login?error=invalid_callback" {
				t.Errorf("unexpected redirect: %s", loc)
			}
		})
	}
}

func TestOIDCCallbackHandler_MissingStateCookie(t *testing.T) {
	setupTestDB(t)

	router := gin.New()
	router.GET("/api/v1/auth/oidc/:id/callback", OIDCCallbackHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/prov-1/callback?code=abc&state=xyz", nil)
	// No cookie set
	router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "/login?error=missing_state" {
		t.Errorf("unexpected redirect: %s", loc)
	}
}

func TestOIDCAuthorizeHandler_ProviderNotFound(t *testing.T) {
	setupTestDB(t)

	router := gin.New()
	router.GET("/api/v1/auth/oidc/:id/authorize", OIDCAuthorizeHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/nonexistent/authorize", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}
