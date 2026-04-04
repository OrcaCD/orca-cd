package routes

import (
	"bytes"
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

func createTestProvider(t *testing.T, name string, enabled bool) models.OIDCProvider {
	t.Helper()
	p := models.OIDCProvider{
		Name:         name,
		IssuerURL:    "https://idp.example.com",
		ClientID:     "client-id",
		ClientSecret: crypto.EncryptedString("client-secret"),
		Scopes:       "groups",
		Enabled:      true,
	}
	if err := gorm.G[models.OIDCProvider](db.DB).Create(t.Context(), &p); err != nil {
		t.Fatalf("failed to create test provider: %v", err)
	}
	if !enabled {
		db.DB.Model(&models.OIDCProvider{}).Where("id = ?", p.Id).Update("enabled", false)
		p.Enabled = false
	}
	return p
}

func TestAdminListOIDCProvidersHandler_Empty(t *testing.T) {
	setupTestDB(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/oidc-providers", nil)

	AdminListOIDCProvidersHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body []oidcProviderResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(body) != 0 {
		t.Errorf("expected 0 providers, got %d", len(body))
	}
}

func TestAdminListOIDCProvidersHandler_ReturnsAll(t *testing.T) {
	setupTestDB(t)

	createTestProvider(t, "Provider A", true)
	createTestProvider(t, "Provider B", false)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/oidc-providers", nil)

	AdminListOIDCProvidersHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body []oidcProviderResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	// Admin list returns all providers (enabled and disabled)
	if len(body) != 2 {
		t.Errorf("expected 2 providers, got %d", len(body))
	}
}

func TestAdminGetOIDCProviderHandler_Found(t *testing.T) {
	setupTestDB(t)
	p := createTestProvider(t, "My IDP", true)

	router := gin.New()
	router.GET("/api/v1/admin/oidc-providers/:id", AdminGetOIDCProviderHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/oidc-providers/"+p.Id, nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body oidcProviderResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Name != "My IDP" {
		t.Errorf("expected name %q, got %q", "My IDP", body.Name)
	}
	if body.ClientID != "client-id" {
		t.Errorf("expected clientId %q, got %q", "client-id", body.ClientID)
	}
}

func TestAdminGetOIDCProviderHandler_NotFound(t *testing.T) {
	setupTestDB(t)

	router := gin.New()
	router.GET("/api/v1/admin/oidc-providers/:id", AdminGetOIDCProviderHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/oidc-providers/nonexistent", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminCreateOIDCProviderHandler_InvalidRequest(t *testing.T) {
	setupTestDB(t)

	tests := []struct {
		name string
		body any
	}{
		{"empty body", nil},
		{"missing name", map[string]any{"issuerUrl": "https://example.com", "clientId": "c", "clientSecret": "s"}},
		{"missing issuerUrl", map[string]any{"name": "IDP", "clientId": "c", "clientSecret": "s"}},
		{"missing clientId", map[string]any{"name": "IDP", "issuerUrl": "https://example.com", "clientSecret": "s"}},
		{"missing clientSecret", map[string]any{"name": "IDP", "issuerUrl": "https://example.com", "clientId": "c"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var reqBody []byte
			if tt.body != nil {
				reqBody, _ = json.Marshal(tt.body)
			}

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/oidc-providers", bytes.NewReader(reqBody))
			c.Request.Header.Set("Content-Type", "application/json")

			AdminCreateOIDCProviderHandler(c)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestAdminUpdateOIDCProviderHandler_NotFound(t *testing.T) {
	setupTestDB(t)

	reqBody, _ := json.Marshal(updateOIDCProviderRequest{ //nolint:gosec // test data
		Name:      "Updated",
		IssuerURL: "https://idp.example.com",
		ClientID:  "client-id",
	})

	router := gin.New()
	router.PUT("/api/v1/admin/oidc-providers/:id", AdminUpdateOIDCProviderHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/oidc-providers/nonexistent", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminUpdateOIDCProviderHandler_InvalidRequest(t *testing.T) {
	setupTestDB(t)
	p := createTestProvider(t, "Test IDP", true)

	router := gin.New()
	router.PUT("/api/v1/admin/oidc-providers/:id", AdminUpdateOIDCProviderHandler)

	// Missing required fields
	reqBody, _ := json.Marshal(map[string]any{"name": "Updated"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/oidc-providers/"+p.Id, bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminUpdateOIDCProviderHandler_SameIssuer(t *testing.T) {
	setupTestDB(t)
	p := createTestProvider(t, "Test IDP", true)

	router := gin.New()
	router.PUT("/api/v1/admin/oidc-providers/:id", AdminUpdateOIDCProviderHandler)

	enabled := true
	reqBody, _ := json.Marshal(updateOIDCProviderRequest{ //nolint:gosec // test data
		Name:      "Updated Name",
		IssuerURL: p.IssuerURL, // same issuer — no OIDC discovery needed
		ClientID:  "new-client-id",
		Enabled:   &enabled,
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/oidc-providers/"+p.Id, bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body oidcProviderResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Name != "Updated Name" {
		t.Errorf("expected name %q, got %q", "Updated Name", body.Name)
	}
	if body.ClientID != "new-client-id" {
		t.Errorf("expected clientId %q, got %q", "new-client-id", body.ClientID)
	}
}

func TestAdminDeleteOIDCProviderHandler_Success(t *testing.T) {
	setupTestDB(t)
	p := createTestProvider(t, "Delete Me", true)

	router := gin.New()
	router.DELETE("/api/v1/admin/oidc-providers/:id", AdminDeleteOIDCProviderHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/oidc-providers/"+p.Id, nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify it's actually deleted
	_, err := gorm.G[models.OIDCProvider](db.DB).Where("id = ?", p.Id).First(t.Context())
	if err == nil {
		t.Error("expected provider to be deleted from DB")
	}
}

func TestAdminDeleteOIDCProviderHandler_NotFound(t *testing.T) {
	setupTestDB(t)

	router := gin.New()
	router.DELETE("/api/v1/admin/oidc-providers/:id", AdminDeleteOIDCProviderHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/oidc-providers/nonexistent", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}
