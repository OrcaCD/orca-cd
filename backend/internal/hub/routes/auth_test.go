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
	if err := testDB.AutoMigrate(&models.User{}, &models.OIDCProvider{}, &models.UserOIDCIdentity{}); err != nil {
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

func setUserClaims(t *testing.T, c *gin.Context, user *models.User) {
	t.Helper()

	token, err := auth.GenerateUserToken(user)
	if err != nil {
		t.Fatalf("GenerateUserToken() error: %v", err)
	}

	claims, err := auth.ValidateUserToken(token)
	if err != nil {
		t.Fatalf("ValidateUserToken() error: %v", err)
	}

	auth.SetClaims(c, claims)
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
	db.DB.Create(&models.User{Email: "test@example.com", Name: "Test", PasswordHash: &hash})

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
	db.DB.Create(&models.User{Email: "test@example.com", Name: "Test", PasswordHash: &hash})

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
	db.DB.Create(&models.User{Email: "test@example.com", Name: "Test", PasswordHash: &hash, PasswordChangeRequired: true})

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

func TestChangePasswordHandler_SuccessClearsRequirement(t *testing.T) {
	setupTestDB(t)

	hash, _ := auth.HashPassword("password123")
	user := models.User{
		Base:                   models.Base{Id: "user-change-password"},
		Email:                  "test@example.com",
		Name:                   "Test",
		PasswordHash:           &hash,
		PasswordChangeRequired: true,
	}
	if err := db.DB.Create(&user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	token, err := auth.GenerateUserToken(&user)
	if err != nil {
		t.Fatalf("GenerateUserToken() error: %v", err)
	}
	claims, err := auth.ValidateUserToken(token)
	if err != nil {
		t.Fatalf("ValidateUserToken() error: %v", err)
	}

	reqBody, _ := json.Marshal(changePasswordRequest{
		CurrentPassword: "password123",
		NewPassword:     "new-password-123",
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/change-password", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	auth.SetClaims(c, claims)

	ChangePasswordHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !hasAuthCookie(w) {
		t.Fatal("expected updated auth cookie in response")
	}

	updated, err := gorm.G[models.User](db.DB).Where("id = ?", user.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load updated user: %v", err)
	}
	if updated.PasswordHash == nil || !auth.CheckPassword("new-password-123", *updated.PasswordHash) {
		t.Fatal("expected password to be updated")
	}
	if updated.PasswordChangeRequired {
		t.Fatal("expected passwordChangeRequired=false after password change")
	}
}

func TestChangePasswordHandler_WrongCurrentPassword(t *testing.T) {
	setupTestDB(t)

	hash, _ := auth.HashPassword("password123")
	user := models.User{Base: models.Base{Id: "user-change-password"}, Email: "test@example.com", Name: "Test", PasswordHash: &hash}
	if err := db.DB.Create(&user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	token, err := auth.GenerateUserToken(&user)
	if err != nil {
		t.Fatalf("GenerateUserToken() error: %v", err)
	}
	claims, err := auth.ValidateUserToken(token)
	if err != nil {
		t.Fatalf("ValidateUserToken() error: %v", err)
	}

	reqBody, _ := json.Marshal(changePasswordRequest{
		CurrentPassword: "wrong-password",
		NewPassword:     "new-password-123",
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/change-password", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	auth.SetClaims(c, claims)

	ChangePasswordHandler(c)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestChangePasswordHandler_RejectsSamePassword(t *testing.T) {
	setupTestDB(t)

	hash, _ := auth.HashPassword("password123")
	user := models.User{Base: models.Base{Id: "user-change-password"}, Email: "test@example.com", Name: "Test", PasswordHash: &hash}
	if err := db.DB.Create(&user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	token, err := auth.GenerateUserToken(&user)
	if err != nil {
		t.Fatalf("GenerateUserToken() error: %v", err)
	}
	claims, err := auth.ValidateUserToken(token)
	if err != nil {
		t.Fatalf("ValidateUserToken() error: %v", err)
	}

	reqBody, _ := json.Marshal(changePasswordRequest{
		CurrentPassword: "password123",
		NewPassword:     "password123",
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/change-password", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	auth.SetClaims(c, claims)

	ChangePasswordHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestChangePasswordHandler_RejectsManagedUser(t *testing.T) {
	setupTestDB(t)

	user := models.User{Base: models.Base{Id: "user-oidc"}, Email: "oidc@example.com", Name: "OIDC User"}
	if err := db.DB.Create(&user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	token, err := auth.GenerateUserToken(&user)
	if err != nil {
		t.Fatalf("GenerateUserToken() error: %v", err)
	}
	claims, err := auth.ValidateUserToken(token)
	if err != nil {
		t.Fatalf("ValidateUserToken() error: %v", err)
	}

	reqBody, _ := json.Marshal(changePasswordRequest{
		CurrentPassword: "password123",
		NewPassword:     "new-password-123",
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/change-password", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	auth.SetClaims(c, claims)

	ChangePasswordHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLoginHandler_WrongPassword(t *testing.T) {
	setupTestDB(t)

	hash, _ := auth.HashPassword("password123")
	db.DB.Create(&models.User{Email: "test@example.com", Name: "Test", PasswordHash: &hash})

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
	if body.Picture != "" {
		t.Errorf("expected Picture to be empty, got %q", body.Picture)
	}
}

func TestProfileHandler_ReturnsPicture(t *testing.T) {
	setupTestDB(t)

	user := &models.User{Base: models.Base{Id: "user-abc"}, Name: "Alice", Email: "alice@example.com"}
	picture := "https://cdn.example.com/alice.png"
	token, err := auth.GenerateUserTokenWithPicture(user, picture)
	if err != nil {
		t.Fatalf("GenerateUserTokenWithPicture() error: %v", err)
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
	if body.Picture != picture {
		t.Errorf("expected Picture %q, got %q", picture, body.Picture)
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
		Base:                   models.Base{Id: "user-admin"},
		Name:                   "Admin",
		Email:                  "admin@example.com",
		Role:                   models.UserRoleAdmin,
		PasswordChangeRequired: true,
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
	if !body.PasswordChangeRequired {
		t.Error("expected passwordChangeRequired=true")
	}
}

func TestUpdateOwnProfileHandler_Success(t *testing.T) {
	setupTestDB(t)

	hash, _ := auth.HashPassword("password123")
	user := &models.User{Base: models.Base{Id: "user-local-1"}, Email: "user@example.com", Name: "Initial Name", PasswordHash: &hash, Role: models.UserRoleUser}
	if err := db.DB.Create(user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	updatedName := "Updated Name"
	updatedEmail := "new@example.com"

	reqBody, _ := json.Marshal(updateOwnProfileRequest{Name: updatedName, Email: "NEW@Example.COM"}) //nolint:gosec
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/auth/profile", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	setUserClaims(t, c, user)

	UpdateOwnProfileHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !hasAuthCookie(w) {
		t.Error("expected refreshed auth cookie in response")
	}

	var body profileResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Name != updatedName {
		t.Errorf("expected Name %q, got %q", updatedName, body.Name)
	}
	if body.Email != updatedEmail {
		t.Errorf("expected normalized email %q, got %q", updatedEmail, body.Email)
	}

	var reloaded models.User
	if err := db.DB.Where("id = ?", user.Id).First(&reloaded).Error; err != nil {
		t.Fatalf("failed to reload user: %v", err)
	}
	if reloaded.Name != updatedName {
		t.Errorf("expected persisted name %q, got %q", updatedName, reloaded.Name)
	}
	if reloaded.Email != updatedEmail {
		t.Errorf("expected persisted email %q, got %q", updatedEmail, reloaded.Email)
	}
}

func TestUpdateOwnProfileHandler_Conflict(t *testing.T) {
	setupTestDB(t)

	hashA, _ := auth.HashPassword("password123")
	hashB, _ := auth.HashPassword("password456")
	userA := &models.User{Base: models.Base{Id: "user-local-3"}, Email: "first@example.com", Name: "First", PasswordHash: &hashA, Role: models.UserRoleUser}
	userB := &models.User{Base: models.Base{Id: "user-local-4"}, Email: "second@example.com", Name: "Second", PasswordHash: &hashB, Role: models.UserRoleUser}
	if err := db.DB.Create(userA).Error; err != nil {
		t.Fatalf("failed to create user A: %v", err)
	}
	if err := db.DB.Create(userB).Error; err != nil {
		t.Fatalf("failed to create user B: %v", err)
	}

	reqBody, _ := json.Marshal(updateOwnProfileRequest{Name: userA.Name, Email: userB.Email}) //nolint:gosec
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/auth/profile", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	setUserClaims(t, c, userA)

	UpdateOwnProfileHandler(c)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}

	var reloaded models.User
	if err := db.DB.Where("id = ?", userA.Id).First(&reloaded).Error; err != nil {
		t.Fatalf("failed to reload user: %v", err)
	}
	if reloaded.Email != "first@example.com" {
		t.Errorf("expected email to remain unchanged, got %q", reloaded.Email)
	}
}

func TestUpdateOwnPasswordHandler_Success(t *testing.T) {
	setupTestDB(t)

	hash, _ := auth.HashPassword("password123")
	user := &models.User{Base: models.Base{Id: "user-local-5"}, Email: "user@example.com", Name: "User", PasswordHash: &hash, Role: models.UserRoleUser}
	if err := db.DB.Create(user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	reqBody, _ := json.Marshal(updateOwnPasswordRequest{CurrentPassword: "password123", NewPassword: "newpassword123"}) //nolint:gosec
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/auth/profile/password", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	setUserClaims(t, c, user)

	UpdateOwnPasswordHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !hasAuthCookie(w) {
		t.Error("expected refreshed auth cookie in response")
	}

	var reloaded models.User
	if err := db.DB.Where("id = ?", user.Id).First(&reloaded).Error; err != nil {
		t.Fatalf("failed to reload user: %v", err)
	}
	if reloaded.PasswordHash == nil {
		t.Fatal("expected password hash to be set")
	}
	if !auth.CheckPassword("newpassword123", *reloaded.PasswordHash) {
		t.Error("expected password hash to match new password")
	}
}

func TestUpdateOwnPasswordHandler_WrongCurrentPassword(t *testing.T) {
	setupTestDB(t)

	hash, _ := auth.HashPassword("password123")
	user := &models.User{Base: models.Base{Id: "user-local-6"}, Email: "user@example.com", Name: "User", PasswordHash: &hash, Role: models.UserRoleUser}
	if err := db.DB.Create(user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	reqBody, _ := json.Marshal(updateOwnPasswordRequest{CurrentPassword: "wrong-password", NewPassword: "newpassword123"}) //nolint:gosec
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/auth/profile/password", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	setUserClaims(t, c, user)

	UpdateOwnPasswordHandler(c)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}

	var reloaded models.User
	if err := db.DB.Where("id = ?", user.Id).First(&reloaded).Error; err != nil {
		t.Fatalf("failed to reload user: %v", err)
	}
	if reloaded.PasswordHash == nil || !auth.CheckPassword("password123", *reloaded.PasswordHash) {
		t.Error("expected original password hash to remain unchanged")
	}
}

func TestSelfUpdateHandlers_RejectSSOUsers(t *testing.T) {
	setupTestDB(t)

	hash, _ := auth.HashPassword("password123")
	user := &models.User{Base: models.Base{Id: "user-sso-1"}, Email: "user@example.com", Name: "User", PasswordHash: &hash, Role: models.UserRoleUser}
	if err := db.DB.Create(user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	identity := models.UserOIDCIdentity{UserId: user.Id, ProviderId: "provider-1", Subject: "subject-1"}
	if err := db.DB.Create(&identity).Error; err != nil {
		t.Fatalf("failed to create oidc identity: %v", err)
	}

	tests := []struct {
		name    string
		body    any
		handler gin.HandlerFunc
		path    string
	}{
		{
			name:    "profile update",
			body:    updateOwnProfileRequest{Name: "Blocked Name", Email: "blocked@example.com"},
			handler: UpdateOwnProfileHandler,
			path:    "/api/v1/auth/profile",
		},
		{
			name:    "password update",
			body:    updateOwnPasswordRequest{CurrentPassword: "password123", NewPassword: "newpassword123"},
			handler: UpdateOwnPasswordHandler,
			path:    "/api/v1/auth/profile/password",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqBody, _ := json.Marshal(tt.body) //nolint:gosec
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPut, tt.path, bytes.NewReader(reqBody))
			c.Request.Header.Set("Content-Type", "application/json")
			setUserClaims(t, c, user)

			tt.handler(c)

			if w.Code != http.StatusForbidden {
				t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}
