package routes

import (
	"bytes"
	"encoding/json"
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
)

func init() {
	gin.SetMode(gin.TestMode)
}

func setupTestDB(t *testing.T) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	testDB, err := gorm.Open(sqlite.Open(dbPath))
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	if err := testDB.AutoMigrate(&models.User{}); err != nil {
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
}

func TestSetupHandler_WithUsers(t *testing.T) {
	setupTestDB(t)

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
