package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/apitoken"
	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/middleware"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func setupAPITokensTestDB(t *testing.T) {
	t.Helper()
	setupTestDB(t)
	if err := db.DB.AutoMigrate(&models.APIToken{}, &models.AuditLog{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	// AutoMigrate cannot create this index from model tags, see migration 000046.
	if err := db.DB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_api_tokens_user_name ON api_tokens(user_id, name COLLATE NOCASE)`).Error; err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
}

// newAPITokensRouter registers the API token routes like RegisterRoutes does,
// with user as the authenticated user.
func newAPITokensRouter(user *models.User) *gin.Engine {
	router := gin.New()
	router.Use(func(c *gin.Context) {
		auth.SetClaims(c, &auth.UserClaims{Subject: user.Id})
		auth.SetUser(c, user)
		c.Next()
	})
	router.GET("/api/v1/api-tokens", ListAPITokensHandler)
	router.POST("/api/v1/api-tokens", middleware.RequireAdmin(), CreateAPITokenHandler)
	router.DELETE("/api/v1/api-tokens/:id", RevokeAPITokenHandler)
	router.GET("/api/v1/admin/api-tokens", middleware.RequireAdmin(), AdminListAPITokensHandler)
	router.DELETE("/api/v1/admin/api-tokens/:id", middleware.RequireAdmin(), AdminRevokeAPITokenHandler)
	return router
}

func serveAPITokenRequest(t *testing.T, user *models.User, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal body: %v", err)
		}
		reader = bytes.NewReader(data)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	newAPITokensRouter(user).ServeHTTP(w, req)
	return w
}

func validCreateAPITokenBody() map[string]any {
	return map[string]any{
		"name":      "CI",
		"scopes":    []string{"application:deploy"},
		"expiresAt": time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339),
	}
}

func seedAPIToken(t *testing.T, userID, name string) models.APIToken {
	t.Helper()
	generated := apitoken.Generate()
	token := models.APIToken{
		UserId:    userID,
		Name:      name,
		TokenHash: generated.Hash,
		Prefix:    generated.Prefix,
		Scopes:    []models.APITokenScope{"application:read"},
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := gorm.G[models.APIToken](db.DB).Create(t.Context(), &token); err != nil {
		t.Fatalf("failed to seed token: %v", err)
	}
	return token
}

func apiTokenExists(t *testing.T, id string) bool {
	t.Helper()
	count, err := gorm.G[models.APIToken](db.DB).Where("id = ?", id).Count(t.Context(), "*")
	if err != nil {
		t.Fatalf("failed to count tokens: %v", err)
	}
	return count > 0
}

func countAPITokenAuditLogs(t *testing.T, eventType, tokenID string) int64 {
	t.Helper()
	count, err := gorm.G[models.AuditLog](db.DB).
		Where("event_type = ? AND target_type = ? AND target_id = ?", eventType, "api-token", tokenID).
		Count(t.Context(), "*")
	if err != nil {
		t.Fatalf("failed to count audit logs: %v", err)
	}
	return count
}

func TestCreateAPITokenHandler_Success(t *testing.T) {
	setupAPITokensTestDB(t)
	admin := createTestUser(t, "Admin", "admin@example.com", rbac.AdminRoleID, "")

	body := validCreateAPITokenBody()
	body["name"] = "  Deploy from CI  "
	body["scopes"] = []string{"application:deploy", "admin", "application:deploy", "application:read"}
	w := serveAPITokenRequest(t, &admin, http.MethodPost, "/api/v1/api-tokens", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp createAPITokenResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !apitoken.IsWellFormed(resp.Token) {
		t.Fatalf("expected a well-formed token, got %q", resp.Token)
	}
	if resp.Name != "Deploy from CI" {
		t.Errorf("expected the name to be trimmed, got %q", resp.Name)
	}
	if !strings.HasPrefix(resp.Token, resp.Prefix) {
		t.Errorf("expected prefix %q to be the start of the token", resp.Prefix)
	}
	wantScopes := []models.APITokenScope{models.APITokenScopeAdmin, "application:read", "application:deploy"}
	if !slices.Equal(resp.Scopes, wantScopes) {
		t.Errorf("expected scopes %v, got %v", wantScopes, resp.Scopes)
	}
	if resp.LastUsedAt != nil {
		t.Errorf("expected lastUsedAt to be empty, got %v", *resp.LastUsedAt)
	}

	stored, err := gorm.G[models.APIToken](db.DB).Where("id = ?", resp.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to fetch token: %v", err)
	}
	if stored.UserId != admin.Id {
		t.Errorf("expected the token to belong to the admin, got %q", stored.UserId)
	}
	if stored.TokenHash != apitoken.Hash(resp.Token) || stored.TokenHash == resp.Token {
		t.Error("expected only the hash of the token to be stored")
	}
	if countAPITokenAuditLogs(t, "created", resp.Id) != 1 {
		t.Error("expected an audit log entry")
	}
}

func TestCreateAPITokenHandler_RequiresAdmin(t *testing.T) {
	setupAPITokensTestDB(t)
	user := createTestUser(t, "User", "user@example.com", rbac.UserRoleID, "")

	w := serveAPITokenRequest(t, &user, http.MethodPost, "/api/v1/api-tokens", validCreateAPITokenBody())
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	count, err := gorm.G[models.APIToken](db.DB).Count(t.Context(), "*")
	if err != nil || count != 0 {
		t.Fatalf("expected no token to be created, count=%d err=%v", count, err)
	}
}

func TestCreateAPITokenHandler_InvalidRequests(t *testing.T) {
	setupAPITokensTestDB(t)
	admin := createTestUser(t, "Admin", "admin@example.com", rbac.AdminRoleID, "")

	tests := []struct {
		name   string
		modify func(body map[string]any)
	}{
		{"missing name", func(b map[string]any) { delete(b, "name") }},
		{"blank name", func(b map[string]any) { b["name"] = "   " }},
		{"name too long", func(b map[string]any) { b["name"] = strings.Repeat("a", maxAPITokenNameLength+1) }},
		{"missing scopes", func(b map[string]any) { delete(b, "scopes") }},
		{"no scopes", func(b map[string]any) { b["scopes"] = []string{} }},
		{"unknown scope", func(b map[string]any) { b["scopes"] = []string{"application:deploy", "agent:deploy"} }},
		{"missing expiry", func(b map[string]any) { delete(b, "expiresAt") }},
		{"invalid expiry", func(b map[string]any) { b["expiresAt"] = "next week" }},
		{"expiry in the past", func(b map[string]any) { b["expiresAt"] = time.Now().Add(-time.Minute).Format(time.RFC3339) }},
		{"expiry too far away", func(b map[string]any) { b["expiresAt"] = time.Now().AddDate(1, 0, 2).Format(time.RFC3339) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := validCreateAPITokenBody()
			tt.modify(body)
			w := serveAPITokenRequest(t, &admin, http.MethodPost, "/api/v1/api-tokens", body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestCreateAPITokenHandler_OneYearExpiry(t *testing.T) {
	setupAPITokensTestDB(t)
	admin := createTestUser(t, "Admin", "admin@example.com", rbac.AdminRoleID, "")

	body := validCreateAPITokenBody()
	body["expiresAt"] = time.Now().AddDate(1, 0, 0).Format(time.RFC3339)
	w := serveAPITokenRequest(t, &admin, http.MethodPost, "/api/v1/api-tokens", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateAPITokenHandler_DuplicateName(t *testing.T) {
	setupAPITokensTestDB(t)
	admin := createTestUser(t, "Admin", "admin@example.com", rbac.AdminRoleID, "")
	other := createTestUser(t, "Other", "other@example.com", rbac.AdminRoleID, "")
	seedAPIToken(t, admin.Id, "CI")

	body := validCreateAPITokenBody()
	body["name"] = "ci"
	w := serveAPITokenRequest(t, &admin, http.MethodPost, "/api/v1/api-tokens", body)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}

	w = serveAPITokenRequest(t, &other, http.MethodPost, "/api/v1/api-tokens", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected another user to be able to use the name, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListAPITokensHandler_OnlyOwnTokens(t *testing.T) {
	setupAPITokensTestDB(t)
	user := createTestUser(t, "User", "user@example.com", rbac.UserRoleID, "")
	other := createTestUser(t, "Other", "other@example.com", rbac.AdminRoleID, "")
	older := seedAPIToken(t, user.Id, "Older")
	newer := seedAPIToken(t, user.Id, "Newer")
	seedAPIToken(t, other.Id, "Not mine")

	w := serveAPITokenRequest(t, &user, http.MethodGet, "/api/v1/api-tokens", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), older.TokenHash) || strings.Contains(w.Body.String(), `"token"`) {
		t.Fatalf("expected the response to contain neither the hash nor a token: %s", w.Body.String())
	}

	var tokens []apiTokenResponse
	if err := json.Unmarshal(w.Body.Bytes(), &tokens); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	ids := make([]string, 0, len(tokens))
	for _, token := range tokens {
		ids = append(ids, token.Id)
		if token.Owner != nil {
			t.Errorf("expected no owner in the own list, got %+v", token.Owner)
		}
	}
	if !slices.Equal(ids, []string{newer.Id, older.Id}) {
		t.Fatalf("expected the own tokens newest first, got %v", ids)
	}
}

func TestRevokeAPITokenHandler(t *testing.T) {
	setupAPITokensTestDB(t)
	user := createTestUser(t, "User", "user@example.com", rbac.UserRoleID, "")
	other := createTestUser(t, "Other", "other@example.com", rbac.AdminRoleID, "")
	own := seedAPIToken(t, user.Id, "Mine")
	foreign := seedAPIToken(t, other.Id, "Not mine")

	w := serveAPITokenRequest(t, &user, http.MethodDelete, "/api/v1/api-tokens/"+foreign.Id, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for the token of another user, got %d: %s", w.Code, w.Body.String())
	}
	if !apiTokenExists(t, foreign.Id) {
		t.Fatal("expected the token of another user to be kept")
	}

	w = serveAPITokenRequest(t, &user, http.MethodDelete, "/api/v1/api-tokens/"+own.Id, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if apiTokenExists(t, own.Id) {
		t.Fatal("expected the token to be deleted")
	}
	if countAPITokenAuditLogs(t, "revoked", own.Id) != 1 {
		t.Error("expected an audit log entry")
	}

	w = serveAPITokenRequest(t, &user, http.MethodDelete, "/api/v1/api-tokens/"+own.Id, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a revoked token, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminListAPITokensHandler_AllTokensWithOwner(t *testing.T) {
	setupAPITokensTestDB(t)
	admin := createTestUser(t, "Admin", "admin@example.com", rbac.AdminRoleID, "")
	user := createTestUser(t, "User", "user@example.com", rbac.UserRoleID, "")
	seedAPIToken(t, admin.Id, "Admin token")
	userToken := seedAPIToken(t, user.Id, "User token")

	w := serveAPITokenRequest(t, &user, http.MethodGet, "/api/v1/admin/api-tokens", nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a non-admin, got %d: %s", w.Code, w.Body.String())
	}

	w = serveAPITokenRequest(t, &admin, http.MethodGet, "/api/v1/admin/api-tokens", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var tokens []apiTokenResponse
	if err := json.Unmarshal(w.Body.Bytes(), &tokens); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(tokens) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(tokens))
	}
	if tokens[0].Id != userToken.Id || tokens[0].Owner == nil || tokens[0].Owner.Email != user.Email {
		t.Fatalf("expected the newest token with its owner first, got %+v", tokens[0])
	}
}

func TestAdminRevokeAPITokenHandler(t *testing.T) {
	setupAPITokensTestDB(t)
	admin := createTestUser(t, "Admin", "admin@example.com", rbac.AdminRoleID, "")
	user := createTestUser(t, "User", "user@example.com", rbac.UserRoleID, "")
	token := seedAPIToken(t, user.Id, "User token")

	w := serveAPITokenRequest(t, &admin, http.MethodDelete, "/api/v1/admin/api-tokens/"+token.Id, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if apiTokenExists(t, token.Id) {
		t.Fatal("expected the token to be deleted")
	}
	if countAPITokenAuditLogs(t, "revoked", token.Id) != 1 {
		t.Error("expected an audit log entry")
	}

	w = serveAPITokenRequest(t, &admin, http.MethodDelete, "/api/v1/admin/api-tokens/unknown", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestValidateAPITokenExpiry(t *testing.T) {
	now := time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		expiresAt time.Time
		wantErr   bool
	}{
		{"now", now, true},
		{"one second ago", now.Add(-time.Second), true},
		{"one minute ahead", now.Add(time.Minute), false},
		{"one year and one day ahead", now.AddDate(1, 0, 1), false},
		{"just over the maximum", now.AddDate(1, 0, 1).Add(time.Second), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAPITokenExpiry(tt.expiresAt, now)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateAPITokenExpiry() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
