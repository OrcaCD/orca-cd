package routes

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func createTestUser(t *testing.T, name string, email string, role models.UserRole, password string) models.User {
	t.Helper()

	var passwordHash *string
	if password != "" {
		hash, err := auth.HashPassword(password)
		if err != nil {
			t.Fatalf("failed to hash password: %v", err)
		}
		passwordHash = &hash
	}

	user := models.User{
		Name:         name,
		Email:        email,
		Role:         role,
		PasswordHash: passwordHash,
	}

	if err := db.DB.WithContext(t.Context()).Create(&user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	return user
}

func TestAdminListUsersHandler_ReturnsUsersWithoutPasswordHash(t *testing.T) {
	setupTestDB(t)

	createTestUser(t, "Admin", "admin@example.com", models.UserRoleAdmin, "password123")
	oidcUser := createTestUser(t, "OIDC User", "oidc@example.com", models.UserRoleUser, "")
	provider := createTestProvider(t, "GitHub", true)
	if err := gorm.G[models.UserOIDCIdentity](db.DB).Create(t.Context(), &models.UserOIDCIdentity{
		UserId:     oidcUser.Id,
		ProviderId: provider.Id,
		Subject:    "oidc-subject",
	}); err != nil {
		t.Fatalf("failed to create oidc identity: %v", err)
	}

	router := gin.New()
	router.GET("/api/v1/admin/users", AdminListUsersHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if strings.Contains(strings.ToLower(w.Body.String()), "password_hash") {
		t.Fatal("response must not include password_hash")
	}

	var body []adminUserResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if len(body) != 2 {
		t.Fatalf("expected 2 users, got %d", len(body))
	}

	byEmail := map[string]adminUserResponse{}
	for _, user := range body {
		byEmail[user.Email] = user
	}

	admin, ok := byEmail["admin@example.com"]
	if !ok {
		t.Fatal("expected admin@example.com in response")
	}
	if !slices.Equal(admin.Providers, []string{"password"}) {
		t.Fatalf("expected admin@example.com providers %v, got %v", []string{"password"}, admin.Providers)
	}
	if admin.PasswordChangeRequired {
		t.Fatal("expected admin@example.com to have passwordChangeRequired=false")
	}

	oidcUserResponse, ok := byEmail["oidc@example.com"]
	if !ok {
		t.Fatal("expected oidc@example.com in response")
	}
	if !slices.Equal(oidcUserResponse.Providers, []string{"GitHub"}) {
		t.Fatalf("expected oidc@example.com providers %v, got %v", []string{"GitHub"}, oidcUserResponse.Providers)
	}
	if oidcUserResponse.PasswordChangeRequired {
		t.Fatal("expected oidc@example.com to have passwordChangeRequired=false")
	}
}

func TestAdminGetUserHandler_NotFound(t *testing.T) {
	setupTestDB(t)

	router := gin.New()
	router.GET("/api/v1/admin/users/:id", AdminGetUserHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users/missing", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminCreateUserHandler_Success(t *testing.T) {
	setupTestDB(t)

	reqBody, _ := json.Marshal(map[string]any{
		"name":  "New User",
		"email": "NEW@Example.com",
		"role":  "user",
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	AdminCreateUserHandler(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var body adminUserWithGeneratedPasswordResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if body.Email != "new@example.com" {
		t.Fatalf("expected normalized email %q, got %q", "new@example.com", body.Email)
	}
	if body.Role != string(models.UserRoleUser) {
		t.Fatalf("expected role %q, got %q", models.UserRoleUser, body.Role)
	}
	if !slices.Equal(body.Providers, []string{"password"}) {
		t.Fatalf("expected providers %v, got %v", []string{"password"}, body.Providers)
	}
	if !body.PasswordChangeRequired {
		t.Fatal("expected passwordChangeRequired=true")
	}
	if body.GeneratedPassword == "" {
		t.Fatal("expected generatedPassword to be returned")
	}

	var user models.User
	if err := db.DB.Where("id = ?", body.Id).First(&user).Error; err != nil {
		t.Fatalf("failed to load created user: %v", err)
	}
	if user.PasswordHash == nil || !auth.CheckPassword(body.GeneratedPassword, *user.PasswordHash) {
		t.Fatal("expected stored password hash to match generated password")
	}
	if !user.PasswordChangeRequired {
		t.Fatal("expected passwordChangeRequired=true for created user")
	}
}

func TestAdminCreateUserHandler_DuplicateEmail(t *testing.T) {
	setupTestDB(t)

	createTestUser(t, "Existing", "existing@example.com", models.UserRoleUser, "password123")

	reqBody, _ := json.Marshal(map[string]any{
		"name":  "Another",
		"email": "existing@example.com",
		"role":  "user",
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	AdminCreateUserHandler(c)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminUpdateUserHandler_Success(t *testing.T) {
	setupTestDB(t)

	user := createTestUser(t, "User", "user@example.com", models.UserRoleUser, "old-password")

	reqBody, _ := json.Marshal(map[string]any{
		"name":  "Updated User",
		"email": "UPDATED@Example.com",
		"role":  "admin",
	})

	router := gin.New()
	router.PUT("/api/v1/admin/users/:id", AdminUpdateUserHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/"+user.Id, bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body adminUserWithGeneratedPasswordResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.GeneratedPassword != "" {
		t.Fatal("expected generatedPassword to be omitted when resetPassword=false")
	}

	updated, err := gorm.G[models.User](db.DB).Where("id = ?", user.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load updated user: %v", err)
	}

	if updated.Name != "Updated User" {
		t.Fatalf("expected updated name, got %q", updated.Name)
	}
	if updated.Email != "updated@example.com" {
		t.Fatalf("expected normalized email %q, got %q", "updated@example.com", updated.Email)
	}
	if updated.Role != models.UserRoleAdmin {
		t.Fatalf("expected role %q, got %q", models.UserRoleAdmin, updated.Role)
	}
	if updated.PasswordHash == nil || !auth.CheckPassword("old-password", *updated.PasswordHash) {
		t.Fatal("expected password to remain unchanged when resetPassword=false")
	}
	if updated.PasswordChangeRequired {
		t.Fatal("expected passwordChangeRequired=false when resetPassword=false")
	}
}

func TestAdminUpdateUserHandler_ResetPassword_Success(t *testing.T) {
	setupTestDB(t)

	user := createTestUser(t, "User", "user@example.com", models.UserRoleUser, "old-password")

	reqBody, _ := json.Marshal(map[string]any{
		"name":          "Updated User",
		"email":         "UPDATED@Example.com",
		"role":          "admin",
		"resetPassword": true,
	})

	router := gin.New()
	router.PUT("/api/v1/admin/users/:id", AdminUpdateUserHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/"+user.Id, bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body adminUserWithGeneratedPasswordResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.GeneratedPassword == "" {
		t.Fatal("expected generatedPassword to be returned when resetPassword=true")
	}
	if !body.PasswordChangeRequired {
		t.Fatal("expected passwordChangeRequired=true when resetPassword=true")
	}

	updated, err := gorm.G[models.User](db.DB).Where("id = ?", user.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load updated user: %v", err)
	}

	if updated.PasswordHash == nil || !auth.CheckPassword(body.GeneratedPassword, *updated.PasswordHash) {
		t.Fatal("expected password to be updated to generated password")
	}
	if !updated.PasswordChangeRequired {
		t.Fatal("expected passwordChangeRequired=true when password is reset")
	}
}

func TestAdminUpdateUserHandler_RejectsDemotingLastAdmin(t *testing.T) {
	setupTestDB(t)

	admin := createTestUser(t, "Admin", "admin@example.com", models.UserRoleAdmin, "password123")

	reqBody, _ := json.Marshal(map[string]any{
		"name":  "Admin",
		"email": "admin@example.com",
		"role":  "user",
	})

	router := gin.New()
	router.PUT("/api/v1/admin/users/:id", AdminUpdateUserHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/"+admin.Id, bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}

	loaded, err := gorm.G[models.User](db.DB).Where("id = ?", admin.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to reload admin user: %v", err)
	}
	if loaded.Role != models.UserRoleAdmin {
		t.Fatalf("expected role to stay %q, got %q", models.UserRoleAdmin, loaded.Role)
	}
}

func TestAdminDeleteUserHandler_Success(t *testing.T) {
	setupTestDB(t)

	createTestUser(t, "Admin", "admin@example.com", models.UserRoleAdmin, "password123")
	user := createTestUser(t, "Delete Me", "delete@example.com", models.UserRoleUser, "password123")

	router := gin.New()
	router.DELETE("/api/v1/admin/users/:id", AdminDeleteUserHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/users/"+user.Id, nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	_, err := gorm.G[models.User](db.DB).Where("id = ?", user.Id).First(t.Context())
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected deleted user to be missing, got err=%v", err)
	}
}

func TestAdminDeleteUserHandler_RejectsDeletingLastAdmin(t *testing.T) {
	setupTestDB(t)

	admin := createTestUser(t, "Admin", "admin@example.com", models.UserRoleAdmin, "password123")

	router := gin.New()
	router.DELETE("/api/v1/admin/users/:id", AdminDeleteUserHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/users/"+admin.Id, nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}

	if err := db.DB.Where("id = ?", admin.Id).First(&models.User{}).Error; err != nil {
		t.Fatalf("expected admin user to remain, got error: %v", err)
	}
}
