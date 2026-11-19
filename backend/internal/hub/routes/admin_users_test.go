package routes

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/passkeys"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func userIsAdmin(t *testing.T, userId string) (bool, error) {
	t.Helper()
	perms, err := rbac.Load(t.Context(), db.DB, userId)
	return perms.IsAdmin(), err
}

// createTestUser creates a user with the given role. An empty roleID creates a user without roles.
func createTestUser(t *testing.T, name string, email string, roleID string, password string) models.User {
	t.Helper()

	user := models.User{Name: name, Email: email, LocalAuthEnabled: password != ""}

	if err := db.DB.WithContext(t.Context()).Create(&user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	if roleID != "" {
		if err := gorm.G[models.UserRole](db.DB).Create(t.Context(), &models.UserRole{UserId: user.Id, RoleId: roleID, Source: models.UserRoleSourceManual}); err != nil {
			t.Fatalf("failed to assign role: %v", err)
		}
	}

	return user
}

func TestAdminListUsersHandler_ReturnsUsersWithoutPasswordHash(t *testing.T) {
	setupTestDB(t)

	createTestUser(t, "Admin", "admin@example.com", rbac.AdminRoleID, "password123")
	oidcUser := createTestUser(t, "OIDC User", "oidc@example.com", rbac.UserRoleID, "")
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
	if !slices.Equal(admin.Providers, []string{"passkey"}) {
		t.Fatalf("expected admin@example.com providers %v, got %v", []string{"passkey"}, admin.Providers)
	}
	if !admin.IsLocal {
		t.Fatal("expected local user")
	}

	oidcUserResponse, ok := byEmail["oidc@example.com"]
	if !ok {
		t.Fatal("expected oidc@example.com in response")
	}
	if !slices.Equal(oidcUserResponse.Providers, []string{"GitHub"}) {
		t.Fatalf("expected oidc@example.com providers %v, got %v", []string{"GitHub"}, oidcUserResponse.Providers)
	}
	if oidcUserResponse.IsLocal {
		t.Fatal("expected managed user")
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
		"name":    "New User",
		"email":   "NEW@Example.com",
		"roleIds": []string{rbac.UserRoleID},
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	AdminCreateUserHandler(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var body adminUserWithEnrollmentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if body.Email != "new@example.com" {
		t.Fatalf("expected normalized email %q, got %q", "new@example.com", body.Email)
	}
	if len(body.Roles) != 1 || body.Roles[0].Id != rbac.UserRoleID || !slices.Equal(body.Roles[0].Sources, []models.UserRoleSource{models.UserRoleSourceManual}) {
		t.Fatalf("expected the user role, got %+v", body.Roles)
	}
	if !slices.Equal(body.Providers, []string{"passkey"}) {
		t.Fatalf("expected providers %v, got %v", []string{"passkey"}, body.Providers)
	}
	if !body.IsLocal {
		t.Fatal("expected local account")
	}
	if body.EnrollmentToken == "" {
		t.Fatal("expected enrollmentToken to be returned")
	}

	var user models.User
	if err := db.DB.Where("id = ?", body.Id).First(&user).Error; err != nil {
		t.Fatalf("failed to load created user: %v", err)
	}
	assertEnrollmentCode(t, user.Id, body.EnrollmentToken)
	if !user.LocalAuthEnabled {
		t.Fatal("expected local account")
	}
}

func TestAdminCreateUserHandler_DuplicateEmail(t *testing.T) {
	setupTestDB(t)

	createTestUser(t, "Existing", "existing@example.com", rbac.UserRoleID, "password123")

	reqBody, _ := json.Marshal(map[string]any{
		"name":    "Another",
		"email":   "existing@example.com",
		"roleIds": []string{rbac.UserRoleID},
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

	user := createTestUser(t, "User", "user@example.com", rbac.UserRoleID, "old-password")

	reqBody, _ := json.Marshal(map[string]any{
		"name":    "Updated User",
		"email":   "UPDATED@Example.com",
		"roleIds": []string{rbac.AdminRoleID},
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

	var body adminUserWithEnrollmentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.EnrollmentToken != "" {
		t.Fatal("expected enrollmentToken to be omitted when resetPasskeys=false")
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
	isAdmin, err := userIsAdmin(t, user.Id)
	if err != nil || !isAdmin {
		t.Fatalf("expected user to have the admin role, err=%v", err)
	}
	isUser, err := gorm.G[models.UserRole](db.DB).Where("user_id = ? AND role_id = ?", user.Id, rbac.UserRoleID).Count(t.Context(), "*")
	if err != nil || isUser != 0 {
		t.Fatalf("expected the user role to be removed, count=%d err=%v", isUser, err)
	}
	if !updated.LocalAuthEnabled {
		t.Fatal("expected local account")
	}
	if updated.TokenVersion != user.TokenVersion {
		t.Fatal("profile update revoked sessions")
	}
}

func TestAdminUserClassificationUsesLocalAuthEnabled(t *testing.T) {
	for _, local := range []bool{true, false} {
		t.Run(strconv.FormatBool(local), func(t *testing.T) {
			setupTestDB(t)
			user := models.User{Name: "Original", Email: "original@example.com", LocalAuthEnabled: local}
			if !local {
				user.PasswordHash = new("legacy-hash")
			}
			if err := gorm.G[models.User](db.DB).Create(t.Context(), &user); err != nil {
				t.Fatal(err)
			}
			response := toAdminUserResponse(&user, nil, nil)
			if (len(response.Providers) != 0) != local {
				t.Fatalf("provider classification ignored LocalAuthEnabled: %v", response.Providers)
			}
			body, _ := json.Marshal(map[string]any{"name": "Renamed", "email": "renamed@example.com"})
			router := gin.New()
			router.PUT("/users/:id", AdminUpdateUserHandler)
			req := httptest.NewRequest(http.MethodPut, "/users/"+user.Id, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			want := http.StatusBadRequest
			if local {
				want = http.StatusOK
			}
			if w.Code != want {
				t.Fatalf("local=%v: got %d, want %d: %s", local, w.Code, want, w.Body.String())
			}
			updated, err := gorm.G[models.User](db.DB).Where("id = ?", user.Id).First(t.Context())
			if err != nil || (updated.Name == "Renamed") != local {
				t.Fatalf("unexpected admin update: %+v %v", updated, err)
			}
		})
	}
}

func TestAdminUpdateUserHandler_ResetPasskeys_Success(t *testing.T) {
	setupTestDB(t)

	user := createTestUser(t, "User", "user@example.com", rbac.UserRoleID, "old-password")

	reqBody, _ := json.Marshal(map[string]any{
		"name":          "Updated User",
		"email":         "UPDATED@Example.com",
		"roleIds":       []string{rbac.AdminRoleID},
		"resetPasskeys": true,
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

	var body adminUserWithEnrollmentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.EnrollmentToken == "" {
		t.Fatal("expected enrollmentToken to be returned when resetPasskeys=true")
	}
	if !body.IsLocal {
		t.Fatal("expected local account")
	}

	updated, err := gorm.G[models.User](db.DB).Where("id = ?", user.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load updated user: %v", err)
	}

	if !updated.LocalAuthEnabled {
		t.Fatal("expected local account")
	}
	assertEnrollmentCode(t, updated.Id, body.EnrollmentToken)
	if updated.TokenVersion != user.TokenVersion+1 {
		t.Fatalf("expected token version %d after reset, got %d", user.TokenVersion+1, updated.TokenVersion)
	}
}

func TestAdminUpdateUserHandler_RejectsDemotingLastAdmin(t *testing.T) {
	setupTestDB(t)

	admin := createTestUser(t, "Admin", "admin@example.com", rbac.AdminRoleID, "password123")

	reqBody, _ := json.Marshal(map[string]any{
		"name":    "Admin",
		"email":   "admin@example.com",
		"roleIds": []string{rbac.UserRoleID},
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

	isAdmin, err := userIsAdmin(t, admin.Id)
	if err != nil || !isAdmin {
		t.Fatalf("expected admin role to stay, err=%v", err)
	}
}

func TestAdminDeleteUserHandler_Success(t *testing.T) {
	setupTestDB(t)

	createTestUser(t, "Admin", "admin@example.com", rbac.AdminRoleID, "password123")
	user := createTestUser(t, "Delete Me", "delete@example.com", rbac.UserRoleID, "password123")

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

	admin := createTestUser(t, "Admin", "admin@example.com", rbac.AdminRoleID, "password123")

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

func TestAdminUpdateUserHandler_EmailConfirmation(t *testing.T) {
	tests := []struct {
		name          string
		email         string
		wantConfirmed bool
	}{
		{"changing the email confirms it", "new@example.com", true},
		{"keeping the email does not confirm it", "user@example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupTestDB(t)

			user := createTestUser(t, "User", "user@example.com", rbac.UserRoleID, "old-password")
			if err := db.DB.Model(&models.User{}).Where("id = ?", user.Id).Update("email_unconfirmed", true).Error; err != nil {
				t.Fatalf("failed to mark email unconfirmed: %v", err)
			}

			reqBody, _ := json.Marshal(map[string]any{"name": "User", "email": tt.email, "roleIds": []string{rbac.UserRoleID}})
			router := gin.New()
			router.PUT("/api/v1/admin/users/:id", AdminUpdateUserHandler)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/"+user.Id, bytes.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}
			updated, err := gorm.G[models.User](db.DB).Where("id = ?", user.Id).First(t.Context())
			if err != nil {
				t.Fatalf("failed to load updated user: %v", err)
			}
			if updated.EmailUnconfirmed == tt.wantConfirmed {
				t.Fatalf("expected emailUnconfirmed=%v, got %v", !tt.wantConfirmed, updated.EmailUnconfirmed)
			}
		})
	}
}

func TestAdminCreateUserHandler_UnknownRole(t *testing.T) {
	setupTestDB(t)

	reqBody, _ := json.Marshal(map[string]any{
		"name":    "New User",
		"email":   "new@example.com",
		"roleIds": []string{"missing"},
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	AdminCreateUserHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if count, _ := gorm.G[models.User](db.DB).Count(t.Context(), "*"); count != 0 {
		t.Fatalf("expected no user to be created, got %d", count)
	}
}

func TestAdminCreateUserHandler_RoleIds(t *testing.T) {
	setupTestDB(t)

	create := func(email string, body map[string]any) adminUserWithEnrollmentResponse {
		t.Helper()
		body["name"] = "New User"
		body["email"] = email
		reqBody, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", bytes.NewReader(reqBody))
		c.Request.Header.Set("Content-Type", "application/json")
		AdminCreateUserHandler(c)
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var response adminUserWithEnrollmentResponse
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		return response
	}

	if roles := create("default@example.com", map[string]any{}).Roles; len(roles) != 1 || roles[0].Id != rbac.UserRoleID {
		t.Fatalf("expected users without roleIds to get the default role, got %+v", roles)
	}
	if roles := create("none@example.com", map[string]any{"roleIds": []string{}}).Roles; len(roles) != 0 {
		t.Fatalf("expected no roles, got %+v", roles)
	}
	if roles := create("both@example.com", map[string]any{"roleIds": []string{rbac.AdminRoleID, rbac.UserRoleID, rbac.AdminRoleID}}).Roles; len(roles) != 2 || roles[0].Id != rbac.AdminRoleID {
		t.Fatalf("expected the admin and user role, got %+v", roles)
	}
}

func TestAdminUpdateUserHandler_KeepsOIDCRoles(t *testing.T) {
	setupTestDB(t)

	createTestUser(t, "Admin", "admin@example.com", rbac.AdminRoleID, "password123")
	user := createTestUser(t, "User", "user@example.com", rbac.UserRoleID, "password123")
	if err := gorm.G[models.UserRole](db.DB).Create(t.Context(), &models.UserRole{UserId: user.Id, RoleId: rbac.UserRoleID, Source: models.UserRoleSourceOIDC}); err != nil {
		t.Fatalf("failed to assign oidc role: %v", err)
	}

	reqBody, _ := json.Marshal(map[string]any{"name": "User", "email": "user@example.com", "roleIds": []string{}})
	router := gin.New()
	router.PUT("/api/v1/admin/users/:id", AdminUpdateUserHandler)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/"+user.Id, bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body adminUserResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(body.Roles) != 1 || !slices.Equal(body.Roles[0].Sources, []models.UserRoleSource{models.UserRoleSourceOIDC}) {
		t.Fatalf("expected only the role from OIDC to remain, got %+v", body.Roles)
	}
}

func TestAdminUpdateUserHandler_ManagedUserRolesOnly(t *testing.T) {
	setupTestDB(t)

	createTestUser(t, "Admin", "admin@example.com", rbac.AdminRoleID, "password123")
	managed := createTestUser(t, "Managed", "managed@example.com", "", "")

	update := func(body map[string]any) *httptest.ResponseRecorder {
		reqBody, _ := json.Marshal(body)
		router := gin.New()
		router.PUT("/api/v1/admin/users/:id", AdminUpdateUserHandler)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/"+managed.Id, bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		return w
	}

	if w := update(map[string]any{"name": "Renamed", "email": "managed@example.com", "roleIds": []string{}}); w.Code != http.StatusBadRequest {
		t.Fatalf("expected renaming a managed user to fail, got %d: %s", w.Code, w.Body.String())
	}
	if w := update(map[string]any{"name": "Managed", "email": "managed@example.com", "resetPasskeys": true}); w.Code != http.StatusBadRequest {
		t.Fatalf("expected resetting the password of a managed user to fail, got %d: %s", w.Code, w.Body.String())
	}
	if w := update(map[string]any{"name": "Managed", "email": "MANAGED@example.com", "roleIds": []string{rbac.UserRoleID}}); w.Code != http.StatusOK {
		t.Fatalf("expected changing the roles of a managed user to work, got %d: %s", w.Code, w.Body.String())
	}

	// The provider may set a name the validation would reject
	short := createTestUser(t, "Al", "al@example.com", "", "")
	managed = short
	if w := update(map[string]any{"name": "Al", "email": "al@example.com", "roleIds": []string{rbac.UserRoleID}}); w.Code != http.StatusOK {
		t.Fatalf("expected a managed user with a short name to be updatable, got %d: %s", w.Code, w.Body.String())
	}
	if w := update(map[string]any{"roleIds": []string{}}); w.Code != http.StatusOK {
		t.Fatalf("expected name and email to be optional for managed users, got %d: %s", w.Code, w.Body.String())
	}
	user, err := gorm.G[models.User](db.DB).Where("id = ?", short.Id).First(t.Context())
	if err != nil || user.Name != "Al" || user.Email != "al@example.com" {
		t.Fatalf("expected the profile to stay unchanged, got %+v (err=%v)", user, err)
	}
}

func TestAdminUpdateUserHandler_KeepsRolesWithoutRoleIds(t *testing.T) {
	setupTestDB(t)

	createTestUser(t, "Admin", "admin@example.com", rbac.AdminRoleID, "password123")
	local := createTestUser(t, "Local", "local@example.com", rbac.UserRoleID, "password123")

	reqBody, _ := json.Marshal(map[string]any{"name": "Renamed", "email": "local@example.com"})
	router := gin.New()
	router.PUT("/api/v1/admin/users/:id", AdminUpdateUserHandler)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/"+local.Id, bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body adminUserResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Name != "Renamed" || len(body.Roles) != 1 || body.Roles[0].Id != rbac.UserRoleID {
		t.Fatalf("expected the name to change and the roles to stay, got %+v", body)
	}
}

func TestAdminUpdateUserHandler_ValidatesLocalProfile(t *testing.T) {
	setupTestDB(t)

	createTestUser(t, "Admin", "admin@example.com", rbac.AdminRoleID, "password123")
	local := createTestUser(t, "Local", "local@example.com", rbac.UserRoleID, "password123")

	for _, body := range []map[string]any{
		{"name": "Lo", "email": "local@example.com"},
		{"name": "Local", "email": "not-an-email"},
		{"email": "local@example.com"},
		{"name": "Local", "email": "local@example.com", "roleIds": []string{"missing"}},
	} {
		reqBody, _ := json.Marshal(body)
		router := gin.New()
		router.PUT("/api/v1/admin/users/:id", AdminUpdateUserHandler)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/"+local.Id, bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for %v, got %d: %s", body, w.Code, w.Body.String())
		}
	}
}

func assertEnrollmentCode(t *testing.T, userID, token string) {
	t.Helper()
	enrollment, err := gorm.G[models.PasskeyEnrollment](db.DB).Where("user_id = ?", userID).First(t.Context())
	if err != nil || token == "" || enrollment.TokenHash != passkeys.TokenHash(token) {
		t.Fatal("expected a stored enrollment digest")
	}
}
