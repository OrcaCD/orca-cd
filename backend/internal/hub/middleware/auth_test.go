package middleware

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
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

	router := gin.New()
	router.GET("/protected", RequireAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})

	w := serveWithToken(router, http.MethodGet, "/protected", "invalid.token.here")

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
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
}

func TestRequireAuth_RefreshesClaimsFromDatabase(t *testing.T) {
	user := &models.User{Id: "user-123", Name: "Old Name", Email: "old@example.com", Role: models.UserRoleAdmin}
	setupAuthTest(t, user)
	token := generateToken(t, user)

	if _, err := gorm.G[models.User](db.DB).Where("id = ?", user.Id).Updates(t.Context(), models.User{
		Name:  "New Name",
		Email: "new@example.com",
		Role:  models.UserRoleUser,
	}); err != nil {
		t.Fatalf("failed to update user: %v", err)
	}

	var got *auth.UserClaims
	router := gin.New()
	router.GET("/protected", RequireAuth(), func(c *gin.Context) {
		got, _ = auth.GetClaims(c)
		c.JSON(http.StatusOK, gin.H{})
	})

	w := serveWithToken(router, http.MethodGet, "/protected", token)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got.Name != "New Name" || got.Email != "new@example.com" || got.Role != string(models.UserRoleUser) {
		t.Errorf("expected claims refreshed from database, got name=%q email=%q role=%q", got.Name, got.Email, got.Role)
	}
}

func TestRequireAuth_PasswordChangeRequired_BlocksOtherPaths(t *testing.T) {
	user := &models.User{Id: "user-123", Name: "admin", Email: "admin@example.com", PasswordChangeRequired: true}
	setupAuthTest(t, user)
	token := generateToken(t, user)

	router := gin.New()
	router.GET("/protected", RequireAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := serveWithToken(router, http.MethodGet, "/protected", token)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRequireAuth_PasswordChangeRequired_UsesDatabaseState(t *testing.T) {
	user := &models.User{Id: "user-123", Name: "admin", Email: "admin@example.com"}
	setupAuthTest(t, user)
	token := generateToken(t, user)

	if _, err := gorm.G[models.User](db.DB).Where("id = ?", user.Id).Update(t.Context(), "password_change_required", true); err != nil {
		t.Fatalf("failed to update user: %v", err)
	}

	router := gin.New()
	router.GET("/protected", RequireAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := serveWithToken(router, http.MethodGet, "/protected", token)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRequireAuth_PasswordChangeRequired_AllowsChangePasswordPath(t *testing.T) {
	user := &models.User{Id: "user-123", Name: "admin", Email: "admin@example.com", PasswordChangeRequired: true}
	setupAuthTest(t, user)
	token := generateToken(t, user)

	router := gin.New()
	router.POST("/api/v1/auth/change-password", RequireAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := serveWithToken(router, http.MethodPost, "/api/v1/auth/change-password", token)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}
