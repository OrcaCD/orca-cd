package middleware

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/apitoken"
	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/OrcaCD/orca-cd/internal/hub/seclog"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// setupAuthTest initializes auth and a test database containing the given users.
func setupAuthTest(t *testing.T, users ...*models.User) {
	t.Helper()

	if err := auth.Init("test-secret-that-is-long-enough-32chars", "http://localhost:8080"); err != nil {
		t.Fatalf("auth.Init() error: %v", err)
	}

	testDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	if err := testDB.AutoMigrate(&models.User{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	if err := rbac.MigrateForTest(testDB); err != nil {
		t.Fatalf("failed to migrate roles: %v", err)
	}
	for _, user := range users {
		if err := gorm.G[models.User](testDB).Create(t.Context(), user); err != nil {
			t.Fatalf("failed to create user: %v", err)
		}
	}

	old := db.DB
	db.DB = testDB
	t.Cleanup(func() {
		if sqlDB, err := testDB.DB(); err == nil {
			_ = sqlDB.Close()
		}
		db.DB = old
	})
}

func generateToken(t *testing.T, user *models.User) string {
	t.Helper()

	token, err := auth.GenerateUserToken(user)
	if err != nil {
		t.Fatalf("GenerateUserToken() error: %v", err)
	}
	return token
}

func serveWithToken(router *gin.Engine, method, path, token string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	req.AddCookie(&http.Cookie{Name: "orcacd_auth", Value: token}) //nolint:gosec
	router.ServeHTTP(w, req)
	return w
}

func authCookieCleared(w *httptest.ResponseRecorder) bool {
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "orcacd_auth" && cookie.Value == "" && cookie.MaxAge < 0 {
			return true
		}
	}
	return false
}

func TestRequireAuth_ValidToken(t *testing.T) {
	user := &models.User{Id: "user-123", Name: "admin", Email: "admin@example.com"}
	setupAuthTest(t, user)
	token := generateToken(t, user)

	router := gin.New()
	router.GET("/protected", RequireAuth(), func(c *gin.Context) {
		claims, ok := auth.GetClaims(c)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "no claims"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"userId": claims.Subject})
	})

	w := serveWithToken(router, http.MethodGet, "/protected", token)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRequireAuth_MissingCookie(t *testing.T) {
	router := gin.New()
	router.GET("/protected", RequireAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestRequireAuth_InvalidToken(t *testing.T) {
	setupAuthTest(t)
	events := seclog.CaptureForTest(t)

	router := gin.New()
	router.GET("/protected", RequireAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})

	w := serveWithToken(router, http.MethodGet, "/protected", "invalid.token.here")

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
	if !events.Has(seclog.TokenRejected, "invalid_token") {
		t.Error("expected a token_rejected security event")
	}
}

func TestRequireAuth_RejectsDeletedUser(t *testing.T) {
	user := &models.User{Id: "user-123", Name: "admin", Email: "admin@example.com"}
	setupAuthTest(t)
	token := generateToken(t, user)

	router := gin.New()
	router.GET("/protected", RequireAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})

	w := serveWithToken(router, http.MethodGet, "/protected", token)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
	if !authCookieCleared(w) {
		t.Error("expected auth cookie to be cleared")
	}
}

func TestRequireAuth_RejectsOutdatedTokenVersion(t *testing.T) {
	user := &models.User{Id: "user-123", Name: "admin", Email: "admin@example.com"}
	setupAuthTest(t, user)
	events := seclog.CaptureForTest(t)
	token := generateToken(t, user)

	if _, err := gorm.G[models.User](db.DB).Where("id = ?", user.Id).Update(t.Context(), "token_version", user.TokenVersion+1); err != nil {
		t.Fatalf("failed to bump token version: %v", err)
	}

	router := gin.New()
	router.GET("/protected", RequireAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})

	w := serveWithToken(router, http.MethodGet, "/protected", token)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
	if !authCookieCleared(w) {
		t.Error("expected auth cookie to be cleared")
	}
	if !events.Has(seclog.TokenRejected, "revoked_token") {
		t.Error("expected a token_rejected security event")
	}
}

func TestRequireAuth_SetsUserFromDatabase(t *testing.T) {
	user := &models.User{Id: "user-123", Name: "Old Name", Email: "old@example.com"}
	setupAuthTest(t, user)
	token := generateToken(t, user)

	if _, err := gorm.G[models.User](db.DB).Where("id = ?", user.Id).Updates(t.Context(), models.User{
		Name:  "New Name",
		Email: "new@example.com",
	}); err != nil {
		t.Fatalf("failed to update user: %v", err)
	}

	var got *models.User
	router := gin.New()
	router.GET("/protected", RequireAuth(), func(c *gin.Context) {
		got, _ = auth.GetUser(c)
		c.JSON(http.StatusOK, gin.H{})
	})

	w := serveWithToken(router, http.MethodGet, "/protected", token)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got == nil || got.Name != "New Name" || got.Email != "new@example.com" {
		t.Errorf("expected current user from database, got %+v", got)
	}
}

// setupAPITokenAuthTest initializes the auth test database and creates an API
// token with the given scopes for the user.
func setupAPITokenAuthTest(t *testing.T, user *models.User, scopes ...models.APITokenScope) (string, models.APIToken) {
	t.Helper()
	setupAuthTest(t, user)
	if err := db.DB.AutoMigrate(&models.APIToken{}, &models.AuditLog{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return createAPIToken(t, user.Id, time.Now().Add(time.Hour), scopes...)
}

func createAPIToken(t *testing.T, userID string, expiresAt time.Time, scopes ...models.APITokenScope) (string, models.APIToken) {
	t.Helper()
	generated := apitoken.Generate()
	token := models.APIToken{
		UserId:    userID,
		Name:      generated.Prefix,
		TokenHash: generated.Hash,
		Prefix:    generated.Prefix,
		Scopes:    scopes,
		ExpiresAt: expiresAt,
	}
	if err := gorm.G[models.APIToken](db.DB).Create(t.Context(), &token); err != nil {
		t.Fatalf("failed to create api token: %v", err)
	}
	return generated.Plain, token
}

func serveWithAPIToken(router *gin.Engine, method, path, token string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	return w
}

func loadAPIToken(t *testing.T, id string) models.APIToken {
	t.Helper()
	token, err := gorm.G[models.APIToken](db.DB).Where("id = ?", id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load api token: %v", err)
	}
	return token
}

func TestRequireAuth_APIToken(t *testing.T) {
	user := &models.User{Id: "user-123", Name: "CI", Email: "ci@example.com"}
	plain, token := setupAPITokenAuthTest(t, user, "application:read")

	router := gin.New()
	router.GET("/protected", RequireAuth(), func(c *gin.Context) {
		u, _ := auth.GetUser(c)
		apiToken, ok := auth.GetAPIToken(c)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "no api token"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"userId": u.Id, "tokenId": apiToken.Id})
	})

	w := serveWithAPIToken(router, http.MethodGet, "/protected", plain)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	want := `{"tokenId":"` + token.Id + `","userId":"user-123"}`
	if w.Body.String() != want {
		t.Errorf("expected body %s, got %s", want, w.Body.String())
	}
}

func TestRequireAuth_APITokenRejected(t *testing.T) {
	user := &models.User{Id: "user-123", Name: "CI", Email: "ci@example.com"}
	_, _ = setupAPITokenAuthTest(t, user)
	expired, _ := createAPIToken(t, user.Id, time.Now().Add(-time.Minute))
	ownerless, _ := createAPIToken(t, "deleted-user", time.Now().Add(time.Hour))

	router := gin.New()
	router.GET("/protected", RequireAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})

	tests := []struct {
		name   string
		token  string
		reason string
	}{
		{"malformed", "orca_short", ""},
		{"unknown", apitoken.Generate().Plain, "unknown_api_token"},
		{"expired", expired, ""},
		{"owner deleted", ownerless, "unknown_user"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := seclog.CaptureForTest(t)

			w := serveWithAPIToken(router, http.MethodGet, "/protected", tt.token)

			if w.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
			}
			if tt.reason != "" && !events.Has(seclog.TokenRejected, tt.reason) {
				t.Errorf("expected a token_rejected security event with reason %q", tt.reason)
			}
		})
	}
}

func TestRequireAuth_OtherBearerTokenUsesCookie(t *testing.T) {
	user := &models.User{Id: "user-123", Name: "CI", Email: "ci@example.com"}
	_, _ = setupAPITokenAuthTest(t, user)

	router := gin.New()
	router.GET("/protected", RequireAuth(), func(c *gin.Context) {
		if _, ok := auth.GetAPIToken(c); ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "unexpected api token"})
			return
		}
		c.JSON(http.StatusOK, gin.H{})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer token-of-a-proxy")
	req.AddCookie(&http.Cookie{Name: "orcacd_auth", Value: generateToken(t, user)}) //nolint:gosec
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected the session cookie to be used, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRequireAuth_APITokenDatabaseErrors(t *testing.T) {
	for name, table := range map[string]any{"token lookup": &models.APIToken{}, "permissions": &models.UserRole{}} {
		t.Run(name, func(t *testing.T) {
			user := &models.User{Id: "user-123", Name: "CI", Email: "ci@example.com"}
			plain, _ := setupAPITokenAuthTest(t, user)
			if err := db.DB.Migrator().DropTable(table); err != nil {
				t.Fatalf("failed to drop table: %v", err)
			}

			router := gin.New()
			router.GET("/protected", RequireAuth(), func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{})
			})

			if w := serveWithAPIToken(router, http.MethodGet, "/protected", plain); w.Code != http.StatusInternalServerError {
				t.Errorf("expected 500, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestRequireAuth_APITokenIgnoresCookie(t *testing.T) {
	user := &models.User{Id: "user-123", Name: "CI", Email: "ci@example.com"}
	_, _ = setupAPITokenAuthTest(t, user)

	router := gin.New()
	router.GET("/protected", RequireAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+apitoken.Generate().Plain)
	req.AddCookie(&http.Cookie{Name: "orcacd_auth", Value: generateToken(t, user)}) //nolint:gosec
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected an invalid token to be rejected despite a valid session, got %d", w.Code)
	}
}

func TestRequireAuth_APITokenScopes(t *testing.T) {
	user := &models.User{Id: "user-1", Name: "Admin", Email: "admin@example.com"}
	plain, _ := setupAPITokenAuthTest(t, user, "application:deploy")
	adminPlain, _ := createAPIToken(t, user.Id, time.Now().Add(time.Hour), models.APITokenScopeAdmin)
	assignRole(t, user.Id, rbac.AdminRoleID)

	router := gin.New()
	ok := func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{}) }
	router.POST("/applications/:id/deploy", RequireAuth(), RequirePermission(models.ResourceApplication, models.ActionDeploy), ok)
	router.DELETE("/applications/:id", RequireAuth(), RequirePermission(models.ResourceApplication, models.ActionDelete), ok)
	router.GET("/admin", RequireAuth(), RequireAdmin(), ok)

	tests := []struct {
		name, token, method, path string
		want                      int
	}{
		{"scope", plain, http.MethodPost, "/applications/app-1/deploy", http.StatusOK},
		{"missing scope", plain, http.MethodDelete, "/applications/app-1", http.StatusForbidden},
		{"admin route without admin scope", plain, http.MethodGet, "/admin", http.StatusForbidden},
		{"admin scope", adminPlain, http.MethodGet, "/admin", http.StatusOK},
		{"admin scope allows everything", adminPlain, http.MethodDelete, "/applications/app-1", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if w := serveWithAPIToken(router, tt.method, tt.path, tt.token); w.Code != tt.want {
				t.Errorf("expected %d, got %d: %s", tt.want, w.Code, w.Body.String())
			}
		})
	}
}

func TestRequireAuth_APITokenMarksUse(t *testing.T) {
	user := &models.User{Id: "user-123", Name: "CI", Email: "ci@example.com"}
	plain, token := setupAPITokenAuthTest(t, user)

	router := gin.New()
	router.GET("/protected", RequireAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})

	countFirstUses := func() int64 {
		t.Helper()
		count, err := gorm.G[models.AuditLog](db.DB).
			Where("event_type = ? AND target_type = ? AND target_id = ? AND user_id = ? AND api_token_id = ?", "first-used-api-token", "api-token", token.Id, user.Id, token.Id).
			Count(t.Context(), "*")
		if err != nil {
			t.Fatalf("failed to count audit logs: %v", err)
		}
		return count
	}

	serveWithAPIToken(router, http.MethodGet, "/protected", plain)
	first := loadAPIToken(t, token.Id).LastUsedAt
	if first == nil {
		t.Fatal("expected last_used_at to be set")
	}
	if countFirstUses() != 1 {
		t.Fatal("expected the first use to be recorded in the audit log")
	}

	serveWithAPIToken(router, http.MethodGet, "/protected", plain)
	if second := loadAPIToken(t, token.Id).LastUsedAt; second == nil || !second.Equal(*first) {
		t.Errorf("expected last_used_at not to be written again within %s", apiTokenUsedInterval)
	}

	old := time.Now().Add(-2 * apiTokenUsedInterval).UTC()
	if _, err := gorm.G[models.APIToken](db.DB).Where("id = ?", token.Id).Update(t.Context(), "last_used_at", old); err != nil {
		t.Fatalf("failed to update last_used_at: %v", err)
	}
	serveWithAPIToken(router, http.MethodGet, "/protected", plain)
	if third := loadAPIToken(t, token.Id).LastUsedAt; third == nil || !third.After(old) {
		t.Error("expected last_used_at to be updated after the interval")
	}
	if countFirstUses() != 1 {
		t.Error("expected only the first use to be recorded")
	}
}

func TestRequireSession(t *testing.T) {
	user := &models.User{Id: "user-123", Name: "CI", Email: "ci@example.com"}
	plain, _ := setupAPITokenAuthTest(t, user)
	events := seclog.CaptureForTest(t)

	router := gin.New()
	router.GET("/session", RequireAuth(), RequireSession(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})

	if w := serveWithToken(router, http.MethodGet, "/session", generateToken(t, user)); w.Code != http.StatusOK {
		t.Errorf("expected the session to be allowed, got %d: %s", w.Code, w.Body.String())
	}
	if w := serveWithAPIToken(router, http.MethodGet, "/session", plain); w.Code != http.StatusForbidden {
		t.Errorf("expected the api token to be rejected, got %d: %s", w.Code, w.Body.String())
	}
	if !events.Has(seclog.AccessDenied, "session_required") {
		t.Error("expected an access_denied security event")
	}
}

func TestRequireAuth_APITokenUsePublishesEvent(t *testing.T) {
	user := &models.User{Id: "user-123", Name: "CI", Email: "ci@example.com"}
	plain, _ := setupAPITokenAuthTest(t, user)
	log := zerolog.Nop()
	broker := sse.NewBroker(&log)
	sse.DefaultBroker = broker
	t.Cleanup(func() { sse.DefaultBroker = nil })
	connID, ch := broker.Subscribe()
	defer broker.Unsubscribe(connID)

	router := gin.New()
	router.GET("/protected", RequireAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})

	serveWithAPIToken(router, http.MethodGet, "/protected", plain)
	select {
	case event := <-ch:
		if urls := event.URLs(); !slices.Equal(urls, []string{"/api/v1/api-tokens", "/api/v1/admin/api-tokens"}) {
			t.Errorf("expected the token lists to be reloaded, got %v", urls)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for SSE event")
	}

	serveWithAPIToken(router, http.MethodGet, "/protected", plain)
	select {
	case event := <-ch:
		t.Errorf("expected no event while last_used_at isn't written, got %v", event.URLs())
	default:
	}
}
