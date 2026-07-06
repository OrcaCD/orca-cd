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

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v) //nolint:errcheck,gosec
}

func newTestOIDCDiscoveryServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"issuer":                                srv.URL,
			"authorization_endpoint":                srv.URL + "/authorize",
			"token_endpoint":                        srv.URL + "/token",
			"jwks_uri":                              srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"keys": []any{}})
	})
	return srv
}

func createTestProvider(t *testing.T, name string, enabled bool) models.OIDCProvider {
	t.Helper()
	p := models.OIDCProvider{
		Name:                 name,
		IssuerURL:            "https://idp.example.com",
		ClientId:             "client-id",
		ClientSecret:         crypto.EncryptedString("client-secret"),
		Scopes:               "groups",
		Enabled:              true,
		RequireVerifiedEmail: false,
		AutoSignup:           true,
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
	OIDCAppURL = "https://app.example.com"
	t.Cleanup(func() {
		OIDCAppURL = ""
	})

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
	if body.ClientId != "client-id" {
		t.Errorf("expected clientId %q, got %q", "client-id", body.ClientId)
	}
	if body.CallbackURL != "https://app.example.com/api/v1/auth/oidc/"+p.Id+"/callback" {
		t.Errorf(
			"expected callbackUrl %q, got %q",
			"https://app.example.com/api/v1/auth/oidc/"+p.Id+"/callback",
			body.CallbackURL,
		)
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

			var body map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			if body["error"] != "invalid request: name, issuerUrl, clientId, and clientSecret are required and must be valid" {
				t.Errorf("unexpected error message: %q", body["error"])
			}
		})
	}
}

func TestAdminUpdateOIDCProviderHandler_NotFound(t *testing.T) {
	setupTestDB(t)

	reqBody, _ := json.Marshal(updateOIDCProviderRequest{ //nolint:gosec // test data
		Name:      "Updated",
		IssuerURL: "https://idp.example.com",
		ClientId:  "client-id",
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

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["error"] != "invalid request: name, issuerUrl, and clientId are required and must be valid" {
		t.Errorf("unexpected error message: %q", body["error"])
	}
}

func TestAdminUpdateOIDCProviderHandler_SameIssuer(t *testing.T) {
	setupTestDB(t)
	p := createTestProvider(t, "Test IDP", true)

	router := gin.New()
	router.PUT("/api/v1/admin/oidc-providers/:id", AdminUpdateOIDCProviderHandler)

	enabled := true
	requireVerifiedEmail := true
	autoSignup := false
	updatedName := "Updated Name"
	reqBody, _ := json.Marshal(updateOIDCProviderRequest{ //nolint:gosec // test data
		Name:                 updatedName,
		IssuerURL:            p.IssuerURL, // same issuer — no OIDC discovery needed
		ClientId:             "new-client-id",
		Enabled:              &enabled,
		RequireVerifiedEmail: &requireVerifiedEmail,
		AutoSignup:           &autoSignup,
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
	if body.Name != updatedName {
		t.Errorf("expected name %q, got %q", updatedName, body.Name)
	}
	if body.ClientId != "new-client-id" {
		t.Errorf("expected clientId %q, got %q", "new-client-id", body.ClientId)
	}
	if !body.RequireVerifiedEmail {
		t.Error("expected requireVerifiedEmail=true")
	}
	if body.AutoSignup {
		t.Error("expected autoSignup=false")
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

func TestAdminCreateOIDCProviderHandler_Success(t *testing.T) {
	setupTestDB(t)
	srv := newTestOIDCDiscoveryServer(t)
	OIDCAppURL = "https://app.example.com"
	t.Cleanup(func() {
		OIDCAppURL = ""
	})

	reqBody, _ := json.Marshal(map[string]any{
		"name":                 "Test IDP",
		"issuerUrl":            srv.URL,
		"clientId":             "my-client",
		"clientSecret":         "my-secret",
		"scopes":               "groups",
		"enabled":              true,
		"requireVerifiedEmail": true,
		"autoSignup":           false,
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/oidc-providers", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	AdminCreateOIDCProviderHandler(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var body oidcProviderResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Name != "Test IDP" {
		t.Errorf("expected name %q, got %q", "Test IDP", body.Name)
	}
	if body.IssuerURL != srv.URL {
		t.Errorf("expected issuerUrl %q, got %q", srv.URL, body.IssuerURL)
	}
	if body.ClientId != "my-client" {
		t.Errorf("expected clientId %q, got %q", "my-client", body.ClientId)
	}
	if body.Scopes != "groups" {
		t.Errorf("expected scopes %q, got %q", "groups", body.Scopes)
	}
	if !body.Enabled {
		t.Error("expected enabled=true")
	}
	if !body.RequireVerifiedEmail {
		t.Error("expected requireVerifiedEmail=true")
	}
	if body.AutoSignup {
		t.Error("expected autoSignup=false")
	}
	if body.Id == "" {
		t.Error("expected non-empty id")
	}
	if body.CallbackURL != "https://app.example.com/api/v1/auth/oidc/"+body.Id+"/callback" {
		t.Errorf(
			"expected callbackUrl %q, got %q",
			"https://app.example.com/api/v1/auth/oidc/"+body.Id+"/callback",
			body.CallbackURL,
		)
	}

	// Verify the secret was encrypted in DB (not stored as plaintext)
	provider, err := gorm.G[models.OIDCProvider](db.DB).Where("id = ?", body.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to find provider: %v", err)
	}
	if provider.ClientSecret.String() != "my-secret" {
		t.Errorf("expected client secret to decrypt to %q, got %q", "my-secret", provider.ClientSecret.String())
	}
}

func TestAdminCreateOIDCProviderHandler_InvalidIssuer(t *testing.T) {
	setupTestDB(t)

	reqBody, _ := json.Marshal(map[string]any{
		"name":         "Bad IDP",
		"issuerUrl":    "https://nonexistent.invalid.example.com",
		"clientId":     "c",
		"clientSecret": "s",
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/oidc-providers", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	AdminCreateOIDCProviderHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminCreateOIDCProviderHandler_DefaultEnabled(t *testing.T) {
	setupTestDB(t)
	srv := newTestOIDCDiscoveryServer(t)

	// Omit "enabled" — should default to true
	reqBody, _ := json.Marshal(map[string]any{
		"name":         "Default Enabled",
		"issuerUrl":    srv.URL,
		"clientId":     "c",
		"clientSecret": "s",
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/oidc-providers", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	AdminCreateOIDCProviderHandler(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var body oidcProviderResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !body.Enabled {
		t.Error("expected enabled to default to true when omitted")
	}
	if body.RequireVerifiedEmail {
		t.Error("expected requireVerifiedEmail to default to false when omitted")
	}
	if !body.AutoSignup {
		t.Error("expected autoSignup to default to true when omitted")
	}
}

func TestAdminUpdateOIDCProviderHandler_ChangedIssuer(t *testing.T) {
	setupTestDB(t)
	p := createTestProvider(t, "Original", true)

	newSrv := newTestOIDCDiscoveryServer(t)

	enabled := true
	reqBody, _ := json.Marshal(updateOIDCProviderRequest{ //nolint:gosec // test data
		Name:      "Migrated",
		IssuerURL: newSrv.URL, // different from original
		ClientId:  "new-client",
		Enabled:   &enabled,
	})

	router := gin.New()
	router.PUT("/api/v1/admin/oidc-providers/:id", AdminUpdateOIDCProviderHandler)

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
	if body.IssuerURL != newSrv.URL {
		t.Errorf("expected issuerUrl %q, got %q", newSrv.URL, body.IssuerURL)
	}
	if body.Name != "Migrated" {
		t.Errorf("expected name %q, got %q", "Migrated", body.Name)
	}
}

func TestAdminUpdateOIDCProviderHandler_ChangedIssuerInvalid(t *testing.T) {
	setupTestDB(t)
	p := createTestProvider(t, "Stable", true)

	enabled := true
	reqBody, _ := json.Marshal(updateOIDCProviderRequest{ //nolint:gosec // test data
		Name:      "Broken",
		IssuerURL: "https://nonexistent.invalid.example.com",
		ClientId:  "c",
		Enabled:   &enabled,
	})

	router := gin.New()
	router.PUT("/api/v1/admin/oidc-providers/:id", AdminUpdateOIDCProviderHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/oidc-providers/"+p.Id, bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminUpdateOIDCProviderHandler_UpdatesClientSecret(t *testing.T) {
	setupTestDB(t)
	p := createTestProvider(t, "Secret Update", true)

	newSecret := "brand-new-secret"
	reqBody, _ := json.Marshal(updateOIDCProviderRequest{ //nolint:gosec // test data
		Name:         "Secret Update",
		IssuerURL:    p.IssuerURL, // same issuer
		ClientId:     p.ClientId,
		ClientSecret: &newSecret,
	})

	router := gin.New()
	router.PUT("/api/v1/admin/oidc-providers/:id", AdminUpdateOIDCProviderHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/oidc-providers/"+p.Id, bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify the new secret in DB
	updated, err := gorm.G[models.OIDCProvider](db.DB).Where("id = ?", p.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to find provider: %v", err)
	}
	if updated.ClientSecret.String() != "brand-new-secret" {
		t.Errorf("expected client secret %q, got %q", "brand-new-secret", updated.ClientSecret.String())
	}
}
