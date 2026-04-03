package routes

import (
	"errors"
	"net/http"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type registerRequest struct {
	Name     string `json:"name" binding:"required,min=2,max=64"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8,max=128"`
}

type loginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type setupResponse struct {
	NeedsSetup bool `json:"needsSetup"`
}

type profileResponse struct {
	Id    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func ProfileHandler(c *gin.Context) {
	claims, ok := auth.GetClaims(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authentication"})
		return
	}

	c.JSON(http.StatusOK, profileResponse{
		Id:    claims.Subject,
		Name:  claims.Name,
		Email: claims.Email,
	})
}

func SetupHandler(c *gin.Context) {
	count, err := gorm.G[models.User](db.DB).Count(c.Request.Context(), "*")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	c.JSON(http.StatusOK, setupResponse{NeedsSetup: count == 0})
}

func RegisterHandler(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: name (min 3 chars), password (min 8 chars) and valid email are required"})
		return
	}

	count, err := gorm.G[models.User](db.DB).Count(c.Request.Context(), "*")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if count > 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": "registration is disabled: a user already exists"})
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	user := models.User{
		Email:        req.Email,
		Name:         req.Name,
		PasswordHash: &hash,
		AuthProvider: models.AuthProviderLocal,
	}
	if err := gorm.G[models.User](db.DB).Create(c.Request.Context(), &user); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "user already exists"})
		return
	}

	token, err := auth.GenerateToken(&user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	auth.SetAuthCookie(c, token)
	c.JSON(http.StatusCreated, gin.H{"message": "user created successfully"})
}

func LoginHandler(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username and password are required"})
		return
	}

	user, err := gorm.G[models.User](db.DB).Where("email = ? AND auth_provider = ?", req.Email, models.AuthProviderLocal).First(c.Request.Context())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid username or password"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	if user.PasswordHash == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid username or password"})
		return
	}

	if !auth.CheckPassword(req.Password, *user.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid username or password"})
		return
	}

	token, err := auth.GenerateToken(&user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	auth.SetAuthCookie(c, token)
	c.JSON(http.StatusOK, gin.H{"message": "login successful"})
}

func LogoutHandler(c *gin.Context) {
	auth.ClearAuthCookie(c)
	c.JSON(http.StatusOK, gin.H{"message": "logged out successfully"})
}
