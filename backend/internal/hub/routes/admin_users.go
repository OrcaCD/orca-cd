package routes

import (
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type adminUserResponse struct {
	Id                     string   `json:"id"`
	Name                   string   `json:"name"`
	Email                  string   `json:"email"`
	Role                   string   `json:"role"`
	Providers              []string `json:"providers"`
	PasswordChangeRequired bool     `json:"passwordChangeRequired"`
	CreatedAt              string   `json:"createdAt"`
	UpdatedAt              string   `json:"updatedAt"`
}

type adminUserWithGeneratedPasswordResponse struct {
	adminUserResponse
	GeneratedPassword string `json:"generatedPassword,omitempty"`
}

type adminCreateUserRequest struct {
	Name  string `json:"name" binding:"required,min=3,max=64"`
	Email string `json:"email" binding:"required,email"`
	Role  string `json:"role" binding:"required,oneof=admin user"`
}

type adminUpdateUserRequest struct {
	Name          string `json:"name" binding:"required,min=3,max=64"`
	Email         string `json:"email" binding:"required,email"`
	Role          string `json:"role" binding:"required,oneof=admin user"`
	ResetPassword bool   `json:"resetPassword"`
}

type adminUserProviderRow struct {
	UserId       string `gorm:"column:user_id"`
	ProviderName string `gorm:"column:provider_name"`
}

func toAdminUserResponse(user *models.User, oidcProviderNames []string) adminUserResponse {
	return adminUserResponse{
		Id:                     user.Id,
		Name:                   user.Name,
		Email:                  user.Email,
		Role:                   string(user.Role),
		Providers:              buildAuthProviderNames(user.PasswordHash, oidcProviderNames),
		PasswordChangeRequired: user.PasswordChangeRequired,
		CreatedAt:              user.CreatedAt.Format(time.RFC3339),
		UpdatedAt:              user.UpdatedAt.Format(time.RFC3339),
	}
}

func toAdminUserWithGeneratedPasswordResponse(user *models.User, oidcProviderNames []string, generatedPassword string) adminUserWithGeneratedPasswordResponse {
	return adminUserWithGeneratedPasswordResponse{
		adminUserResponse: toAdminUserResponse(user, oidcProviderNames),
		GeneratedPassword: generatedPassword,
	}
}

func AdminListUsersHandler(c *gin.Context) {
	var users []models.User
	if err := db.DB.WithContext(c.Request.Context()).Order("created_at ASC").Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	userIds := make([]string, 0, len(users))
	for i := range users {
		userIds = append(userIds, users[i].Id)
	}

	oidcProviderNamesByUserId, err := loadOIDCProviderNamesByUserId(c, userIds)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	response := make([]adminUserResponse, 0, len(users))
	for i := range users {
		response = append(response, toAdminUserResponse(&users[i], oidcProviderNamesByUserId[users[i].Id]))
	}

	c.JSON(http.StatusOK, response)
}

func AdminGetUserHandler(c *gin.Context) {
	id := c.Param("id")

	var user models.User
	if err := db.DB.WithContext(c.Request.Context()).Where("id = ?", id).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	oidcProviderNamesByUserId, err := loadOIDCProviderNamesByUserId(c, []string{user.Id})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, toAdminUserResponse(&user, oidcProviderNamesByUserId[user.Id]))
}

func AdminCreateUserHandler(c *gin.Context) {
	var req adminCreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: name, email and role are required and must be valid"})
		return
	}

	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

	generatedPassword, err := auth.GenerateRandomPassword()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	hash, err := auth.HashPassword(generatedPassword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	user := models.User{
		Name:                   req.Name,
		Email:                  req.Email,
		PasswordHash:           &hash,
		PasswordChangeRequired: true,
		Role:                   models.UserRole(req.Role),
	}

	if err := db.DB.WithContext(c.Request.Context()).Create(&user).Error; err != nil {
		if isUniqueConstraintError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "a user with this email already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusCreated, toAdminUserWithGeneratedPasswordResponse(&user, nil, generatedPassword))
}

func AdminUpdateUserHandler(c *gin.Context) {
	id := c.Param("id")

	var req adminUpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: name, email and role are required and must be valid"})
		return
	}

	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

	var user models.User
	if err := db.DB.WithContext(c.Request.Context()).Where("id = ?", id).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	if user.PasswordHash == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot update a managed user"})
		return
	}

	if user.Role == models.UserRoleAdmin && req.Role != string(models.UserRoleAdmin) {
		count, err := countAdminUsers(c)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			return
		}
		if count <= 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cannot demote the last admin user"})
			return
		}
	}

	user.Name = req.Name
	user.Email = req.Email
	user.Role = models.UserRole(req.Role)

	generatedPassword := ""
	if req.ResetPassword {
		var genErr error
		generatedPassword, genErr = auth.GenerateRandomPassword()
		if genErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			return
		}

		hash, err := auth.HashPassword(generatedPassword)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			return
		}
		user.PasswordHash = &hash
		user.PasswordChangeRequired = true
	}

	if err := db.DB.WithContext(c.Request.Context()).Save(&user).Error; err != nil {
		if isUniqueConstraintError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "a user with this email already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	oidcProviderNamesByUserId, err := loadOIDCProviderNamesByUserId(c, []string{user.Id})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, toAdminUserWithGeneratedPasswordResponse(&user, oidcProviderNamesByUserId[user.Id], generatedPassword))
}

func AdminDeleteUserHandler(c *gin.Context) {
	id := c.Param("id")

	var user models.User
	if err := db.DB.WithContext(c.Request.Context()).Where("id = ?", id).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	if user.Role == models.UserRoleAdmin {
		count, err := countAdminUsers(c)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			return
		}
		if count <= 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cannot delete the last admin user"})
			return
		}
	}

	if err := db.DB.WithContext(c.Request.Context()).Delete(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "user deleted"})
}

func countAdminUsers(c *gin.Context) (int64, error) {
	var count int64
	err := db.DB.WithContext(c.Request.Context()).Model(&models.User{}).Where("role = ?", models.UserRoleAdmin).Count(&count).Error
	return count, err
}

func loadOIDCProviderNamesByUserId(c *gin.Context, userIds []string) (map[string][]string, error) {
	providerNamesByUserId := make(map[string][]string, len(userIds))
	for _, userId := range userIds {
		providerNamesByUserId[userId] = make([]string, 0)
	}

	if len(userIds) == 0 {
		return providerNamesByUserId, nil
	}

	var rows []adminUserProviderRow
	err := db.DB.WithContext(c.Request.Context()).
		Table("user_oidc_identities AS uoi").
		Select("uoi.user_id, op.name AS provider_name").
		Joins("JOIN oidc_providers AS op ON op.id = uoi.provider_id").
		Where("uoi.user_id IN ?", userIds).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		providerName := strings.TrimSpace(row.ProviderName)
		if providerName == "" {
			continue
		}
		providerNamesByUserId[row.UserId] = append(providerNamesByUserId[row.UserId], providerName)
	}

	return providerNamesByUserId, nil
}

func buildAuthProviderNames(passwordHash *string, oidcProviderNames []string) []string {
	seen := make(map[string]struct{}, len(oidcProviderNames)+1)
	providerNames := make([]string, 0, len(oidcProviderNames)+1)

	if passwordHash != nil {
		providerNames = append(providerNames, "password")
		seen["password"] = struct{}{}
	}

	uniqueOIDCProviderNames := make([]string, 0, len(oidcProviderNames))
	for _, providerName := range oidcProviderNames {
		providerName = strings.TrimSpace(providerName)
		if providerName == "" {
			continue
		}
		if _, ok := seen[providerName]; ok {
			continue
		}
		seen[providerName] = struct{}{}
		uniqueOIDCProviderNames = append(uniqueOIDCProviderNames, providerName)
	}

	sort.Strings(uniqueOIDCProviderNames)

	providerNames = append(providerNames, uniqueOIDCProviderNames...)
	return providerNames
}

func isUniqueConstraintError(err error) bool {
	return errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(strings.ToLower(err.Error()), "unique constraint failed")
}
