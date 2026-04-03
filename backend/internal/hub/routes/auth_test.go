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
	db.DB = testDB

	if err := auth.Init("test-secret-that-is-long-enough-32chars", "http://localhost:8080"); err != nil {
		t.Fatalf("failed to init auth: %v", err)
	}
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
	db.DB.Create(&models.User{Username: "admin", PasswordHash: hash, AuthProvider: models.AuthProviderLocal})

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
	reqBody, _ := json.Marshal(registerRequest{Username: "admin", Password: "password123"})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	RegisterHandler(c)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var body tokenResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Token == "" {
		t.Error("expected non-empty token")
	}
}

func TestRegisterHandler_RejectsSecondUser(t *testing.T) {
	setupTestDB(t)

	hash, _ := auth.HashPassword("password123")
	db.DB.Create(&models.User{Username: "admin", PasswordHash: hash, AuthProvider: models.AuthProviderLocal})

	//nolint:gosec
	reqBody, _ := json.Marshal(registerRequest{Username: "hacker", Password: "password456"})
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
		{"short username", registerRequest{Username: "ab", Password: "password123"}},
		{"short password", registerRequest{Username: "admin", Password: "short"}},
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
	db.DB.Create(&models.User{Username: "admin", PasswordHash: hash, AuthProvider: models.AuthProviderLocal})

	//nolint:gosec
	reqBody, _ := json.Marshal(loginRequest{Username: "admin", Password: "password123"})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	LoginHandler(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body tokenResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Token == "" {
		t.Error("expected non-empty token")
	}
}

func TestLoginHandler_WrongPassword(t *testing.T) {
	setupTestDB(t)

	hash, _ := auth.HashPassword("password123")
	db.DB.Create(&models.User{Username: "admin", PasswordHash: hash, AuthProvider: models.AuthProviderLocal})

	//nolint:gosec
	reqBody, _ := json.Marshal(loginRequest{Username: "admin", Password: "wrongpassword"})
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
	reqBody, _ := json.Marshal(loginRequest{Username: "nonexistent", Password: "password123"})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	LoginHandler(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}
