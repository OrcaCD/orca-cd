package routes

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type adminUserResponse struct {
	Id          string `json:"id"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	Role        string `json:"role"`
	HasPassword bool   `json:"hasPassword"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type adminUserWithGeneratedPasswordResponse struct {
	adminUserResponse
	GeneratedPassword string `json:"generatedPassword"`
}

type adminCreateUserRequest struct {
	Name  string `json:"name" binding:"required,min=3,max=64"`
	Email string `json:"email" binding:"required,email"`
	Role  string `json:"role" binding:"required,oneof=admin user"`
}

type adminUpdateUserRequest struct {
	Name  string `json:"name" binding:"required,min=3,max=64"`
	Email string `json:"email" binding:"required,email"`
	Role  string `json:"role" binding:"required,oneof=admin user"`
}

func toAdminUserResponse(user *models.User) adminUserResponse {
	return adminUserResponse{
		Id:          user.Id,
		Name:        user.Name,
		Email:       user.Email,
		Role:        string(user.Role),
		HasPassword: user.PasswordHash != nil,
		CreatedAt:   user.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   user.UpdatedAt.Format(time.RFC3339),
	}
}

func toAdminUserWithGeneratedPasswordResponse(user *models.User, generatedPassword string) adminUserWithGeneratedPasswordResponse {
	return adminUserWithGeneratedPasswordResponse{
		adminUserResponse: toAdminUserResponse(user),
		GeneratedPassword: generatedPassword,
	}
}

func AdminListUsersHandler(c *gin.Context) {
	var users []models.User
	if err := db.DB.WithContext(c.Request.Context()).Order("created_at ASC").Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	response := make([]adminUserResponse, 0, len(users))
	for i := range users {
		response = append(response, toAdminUserResponse(&users[i]))
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

	c.JSON(http.StatusOK, toAdminUserResponse(&user))
}

func AdminCreateUserHandler(c *gin.Context) {
	var req adminCreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: name, email and role are required and must be valid"})
		return
	}

	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

	generatedPassword, err := generateRandomPassword()
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
		Name:         req.Name,
		Email:        req.Email,
		PasswordHash: &hash,
		Role:         models.UserRole(req.Role),
	}

	if err := db.DB.WithContext(c.Request.Context()).Create(&user).Error; err != nil {
		if isUniqueConstraintError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "a user with this email already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusCreated, toAdminUserWithGeneratedPasswordResponse(&user, generatedPassword))
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

	generatedPassword, err := generateRandomPassword()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	hash, err := auth.HashPassword(generatedPassword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	user.PasswordHash = &hash

	if err := db.DB.WithContext(c.Request.Context()).Save(&user).Error; err != nil {
		if isUniqueConstraintError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "a user with this email already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, toAdminUserWithGeneratedPasswordResponse(&user, generatedPassword))
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

func isUniqueConstraintError(err error) bool {
	return errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(strings.ToLower(err.Error()), "unique constraint failed")
}

func generateRandomPassword() (string, error) {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(raw), nil
}
