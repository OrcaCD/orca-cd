package routes

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
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

func setupTestDB(t *testing.T) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	testDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: gormlogger.New(
			log.New(os.Stderr, "\n", log.LstdFlags),
			gormlogger.Config{
				SlowThreshold:             200 * time.Millisecond,
				LogLevel:                  gormlogger.Warn,
				IgnoreRecordNotFoundError: true,
			},
		),
	})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	if err := testDB.AutoMigrate(&models.User{}, &models.OIDCProvider{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	sqlDB, err := testDB.DB()
	if err != nil {
		t.Fatalf("failed to get sql db: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
		db.DB = nil
	})

	db.DB = testDB

	if err := crypto.Init("test-secret-that-is-long-enough-32chars"); err != nil {
		t.Fatalf("failed to init crypto: %v", err)
	}

	if err := auth.Init("test-secret-that-is-long-enough-32chars", "http://localhost:8080"); err != nil {
		t.Fatalf("failed to init auth: %v", err)
	}
}

func hasAuthCookie(w *httptest.ResponseRecorder) bool {
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "orcacd_auth" && cookie.Value != "" {
			return true
		}
	}
	return false
}

func TestSetupHandler_NoUsers(t *testing.T) {
	setupTestDB(t)
	LocalAuthDisabled = false
	t.Cleanup(func() { LocalAuthDisabled = false })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/setup", nil)

	SetupHandler(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var body setupResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !body.NeedsSetup {
		t.Error("expected needsSetup=true when no users exist")
	}
	if len(body.Providers) != 0 {
		t.Errorf("expected 0 providers, got %d", len(body.Providers))
	}
	if !body.LocalAuthEnabled {
		t.Error("expected localAuthEnabled=true")
	}
}

func TestSetupHandler_WithUsers(t *testing.T) {
	setupTestDB(t)
	LocalAuthDisabled = false
	t.Cleanup(func() { LocalAuthDisabled = false })

	hash, _ := auth.HashPassword("password123")
	db.DB.Create(&models.User{Email: "test@example.com", Name: "Test", PasswordHash: &hash, AuthProvider: models.AuthProviderLocal})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/setup", nil)

	SetupHandler(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var body setupResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.NeedsSetup {
		t.Error("expected needsSetup=false when users exist")
	}
}

func TestSetupHandler_ReturnsProvidersAndLocalAuthSetting(t *testing.T) {
	setupTestDB(t)

	if err := gorm.G[models.OIDCProvider](db.DB).Create(t.Context(), &models.OIDCProvider{
		Name:         "Enabled IDP",
		IssuerURL:    "https://idp.example.com",
		ClientId:     "client-1",
		ClientSecret: crypto.EncryptedString("secret"),
		Enabled:      true,
	}); err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	LocalAuthDisabled = true
	t.Cleanup(func() { LocalAuthDisabled = false })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/setup", nil)

	SetupHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body setupResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(body.Providers) != 1 {
		t.Fatalf("expected 1 enabled provider, got %d", len(body.Providers))
	}
	if body.Providers[0].Name != "Enabled IDP" {
		t.Errorf("expected provider name %q, got %q", "Enabled IDP", body.Providers[0].Name)
	}
	if body.LocalAuthEnabled {
		t.Error("expected localAuthEnabled=false when LocalAuthDisabled=true")
	}
}

func TestRegisterHandler_Success(t *testing.T) {
	setupTestDB(t)

	//nolint:gosec
	reqBody, _ := json.Marshal(registerRequest{Name: "Test", Email: "test@example.com", Password: "password123"})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	RegisterHandler(c)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if !hasAuthCookie(w) {
		t.Error("expected auth cookie in response")
	}
}

func TestRegisterHandler_RejectsSecondUser(t *testing.T) {
	setupTestDB(t)

	hash, _ := auth.HashPassword("password123")
	db.DB.Create(&models.User{Email: "test@example.com", Name: "Test", PasswordHash: &hash, AuthProvider: models.AuthProviderLocal})

	//nolint:gosec
	reqBody, _ := json.Marshal(registerRequest{Name: "Hacker", Email: "hacker@example.com", Password: "password456"})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	RegisterHandler(c)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRegisterHandler_InvalidRequest(t *testing.T) {
	setupTestDB(t)

	tests := []struct {
		name string
		body any
	}{
		{"short name", registerRequest{Name: "a", Email: "test@example.com", Password: "password123"}},
		{"short password", registerRequest{Name: "Test", Email: "test@example.com", Password: "short"}},
		{"invalid email", registerRequest{Name: "Test", Email: "notanemail", Password: "password123"}},
		{"empty body", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var reqBody []byte
			if tt.body != nil {
				reqBody, _ = json.Marshal(tt.body)
			}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(reqBody))
			c.Request.Header.Set("Content-Type", "application/json")

			RegisterHandler(c)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestLoginHandler_Success(t *testing.T) {
	setupTestDB(t)

	hash, _ := auth.HashPassword("password123")
	db.DB.Create(&models.User{Email: "test@example.com", Name: "Test", PasswordHash: &hash, AuthProvider: models.AuthProviderLocal})

	//nolint:gosec
	reqBody, _ := json.Marshal(loginRequest{Email: "test@example.com", Password: "password123"})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	LoginHandler(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !hasAuthCookie(w) {
		t.Error("expected auth cookie in response")
	}
}

func TestLoginHandler_WrongPassword(t *testing.T) {
	setupTestDB(t)

	hash, _ := auth.HashPassword("password123")
	db.DB.Create(&models.User{Email: "test@example.com", Name: "Test", PasswordHash: &hash, AuthProvider: models.AuthProviderLocal})

	//nolint:gosec
	reqBody, _ := json.Marshal(loginRequest{Email: "test@example.com", Password: "wrongpassword"})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	LoginHandler(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLoginHandler_UserNotFound(t *testing.T) {
	setupTestDB(t)

	//nolint:gosec
	reqBody, _ := json.Marshal(loginRequest{Email: "nonexistent@example.com", Password: "password123"})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	LoginHandler(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestProfileHandler_Success(t *testing.T) {
	setupTestDB(t)

	user := &models.User{Base: models.Base{Id: "user-abc"}, Name: "Alice", Email: "alice@example.com"}
	token, err := auth.GenerateUserToken(user)
	if err != nil {
		t.Fatalf("GenerateUserToken() error: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/profile", nil)
	c.Request.AddCookie(&http.Cookie{Name: "orcacd_auth", Value: token})

	claims, err := auth.ValidateUserToken(token)
	if err != nil {
		t.Fatalf("ValidateUserToken() error: %v", err)
	}
	auth.SetClaims(c, claims)

	ProfileHandler(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body profileResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Id != "user-abc" {
		t.Errorf("expected Id %q, got %q", "user-abc", body.Id)
	}
	if body.Name != "Alice" {
		t.Errorf("expected Name %q, got %q", "Alice", body.Name)
	}
	if body.Email != "alice@example.com" {
		t.Errorf("expected Email %q, got %q", "alice@example.com", body.Email)
	}
}

func TestProfileHandler_NoClaims(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/profile", nil)

	ProfileHandler(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLoginHandler_LocalAuthDisabled(t *testing.T) {
	setupTestDB(t)

	LocalAuthDisabled = true
	t.Cleanup(func() { LocalAuthDisabled = false })

	//nolint:gosec
	reqBody, _ := json.Marshal(loginRequest{Email: "test@example.com", Password: "password123"})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	LoginHandler(c)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRegisterHandler_AssignsAdminRole(t *testing.T) {
	setupTestDB(t)

	//nolint:gosec
	reqBody, _ := json.Marshal(registerRequest{Name: "First User", Email: "admin@example.com", Password: "password123"})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	RegisterHandler(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Verify the user was created with admin role
	var user models.User
	if err := db.DB.Where("email = ?", "admin@example.com").First(&user).Error; err != nil {
		t.Fatalf("failed to find user: %v", err)
	}
	if user.Role != models.UserRoleAdmin {
		t.Errorf("expected role %q, got %q", models.UserRoleAdmin, user.Role)
	}
}

func TestProfileHandler_ReturnsRole(t *testing.T) {
	setupTestDB(t)

	user := &models.User{
		Base:  models.Base{Id: "user-admin"},
		Name:  "Admin",
		Email: "admin@example.com",
		Role:  models.UserRoleAdmin,
	}
	token, err := auth.GenerateUserToken(user)
	if err != nil {
		t.Fatalf("GenerateUserToken() error: %v", err)
	}

	claims, err := auth.ValidateUserToken(token)
	if err != nil {
		t.Fatalf("ValidateUserToken() error: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/profile", nil)
	auth.SetClaims(c, claims)

	ProfileHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body profileResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Role != "admin" {
		t.Errorf("expected role %q, got %q", "admin", body.Role)
	}
}
