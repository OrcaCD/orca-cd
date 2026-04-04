package routes

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var errRegistrationDisabled = errors.New("registration disabled")

type registerRequest struct {
	Name     string `json:"name" binding:"required,min=3,max=64"`
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

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	var user models.User

	txErr := db.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		count, err := gorm.G[models.User](tx).Count(c.Request.Context(), "*")
		if err != nil {
			return err
		}
		if count > 0 {
			return errRegistrationDisabled
		}

		user = models.User{
			Email:        req.Email,
			Name:         req.Name,
			PasswordHash: &hash,
			AuthProvider: models.AuthProviderLocal,
		}
		return gorm.G[models.User](tx).Create(c.Request.Context(), &user)
	}, &sql.TxOptions{Isolation: sql.LevelSerializable})

	if txErr != nil {
		if errors.Is(txErr, errRegistrationDisabled) || errors.Is(txErr, gorm.ErrDuplicatedKey) {
			c.JSON(http.StatusForbidden, gin.H{"error": "registration is disabled"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	token, err := auth.GenerateUserToken(&user)
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "email and password are required"})
		return
	}

	user, err := gorm.G[models.User](db.DB).Where("email = ? AND auth_provider = ?", req.Email, models.AuthProviderLocal).First(c.Request.Context())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			auth.CompareWithDummy(req.Password)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	if user.PasswordHash == nil {
		auth.CompareWithDummy(req.Password)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
		return
	}

	if !auth.CheckPassword(req.Password, *user.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
		return
	}

	token, err := auth.GenerateUserToken(&user)
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
