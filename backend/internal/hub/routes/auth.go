package routes

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var errRegistrationDisabled = errors.New("registration disabled")

// LocalAuthDisabled is set by the server config to gate local auth endpoints.
var LocalAuthDisabled bool

type registerRequest struct {
	Name     string `json:"name" binding:"required,min=3,max=64"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8,max=128"`
}

type loginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type updateOwnProfileRequest struct {
	Name  string `json:"name" binding:"required,min=3,max=64"`
	Email string `json:"email" binding:"required,email"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword" binding:"required,min=1,max=128"`
	NewPassword     string `json:"newPassword" binding:"required,min=8,max=128"`
}

type providerInfo struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

type setupResponse struct {
	NeedsSetup       bool           `json:"needsSetup"`
	Providers        []providerInfo `json:"providers"`
	LocalAuthEnabled bool           `json:"localAuthEnabled"`
}

type profileResponse struct {
	Id                     string `json:"id"`
	Name                   string `json:"name"`
	Email                  string `json:"email"`
	Picture                string `json:"picture,omitempty"`
	Role                   string `json:"role"`
	PasswordChangeRequired bool   `json:"passwordChangeRequired"`
	IsLocal                bool   `json:"isLocal"`
}

func ProfileHandler(c *gin.Context) {
	claims, ok := auth.GetClaims(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authentication"})
		return
	}

	c.JSON(http.StatusOK, profileResponse{
		Id:                     claims.Subject,
		Name:                   claims.Name,
		Email:                  claims.Email,
		Picture:                claims.Picture,
		Role:                   claims.Role,
		PasswordChangeRequired: claims.PasswordChangeRequired,
		IsLocal:                claims.IsLocal,
	})
}

func getCurrentLocalUser(c *gin.Context) (*auth.UserClaims, *models.User, bool) {
	claims, ok := auth.GetClaims(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authentication"})
		return nil, nil, false
	}

	user, err := gorm.G[models.User](db.DB).Where("id = ?", claims.Subject).First(c.Request.Context())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid authentication"})
			return nil, nil, false
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return nil, nil, false
	}

	if user.PasswordHash == nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "this operation is not available for sso users"})
		return nil, nil, false
	}

	return claims, &user, true
}

func UpdateOwnProfileHandler(c *gin.Context) {
	var req updateOwnProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: name (min 3 chars) and valid email are required"})
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

	claims, user, ok := getCurrentLocalUser(c)
	if !ok {
		return
	}

	if err := db.DB.WithContext(c.Request.Context()).Model(&models.User{}).Where("id = ?", user.Id).Updates(map[string]any{"name": req.Name, "email": req.Email}).Error; err != nil {
		if isUniqueConstraintError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "a user with this email already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	user.Name = req.Name
	user.Email = req.Email

	token, err := auth.GenerateUserTokenWithPicture(user, claims.Picture)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	auth.SetAuthCookie(c, token)
	c.JSON(http.StatusOK, profileResponse{
		Id:                     user.Id,
		Name:                   user.Name,
		Email:                  user.Email,
		Picture:                claims.Picture,
		Role:                   string(user.Role),
		PasswordChangeRequired: user.PasswordChangeRequired,
		IsLocal:                true,
	})
}

func SetupHandler(c *gin.Context) {
	count, err := gorm.G[models.User](db.DB).Count(c.Request.Context(), "*")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	providers, err := gorm.G[models.OIDCProvider](db.DB).Where("enabled = ?", true).Find(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	infos := make([]providerInfo, 0, len(providers))
	for _, p := range providers {
		infos = append(infos, providerInfo{Id: p.Id, Name: p.Name})
	}

	c.JSON(http.StatusOK, setupResponse{
		NeedsSetup:       count == 0,
		Providers:        infos,
		LocalAuthEnabled: !LocalAuthDisabled,
	})
}

func RegisterHandler(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: name (min 3 chars), password (min 8 chars) and valid email are required"})
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

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
			Role:         models.UserRoleAdmin,
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
	if LocalAuthDisabled {
		c.JSON(http.StatusForbidden, gin.H{"error": "local authentication is disabled"})
		return
	}

	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email and password are required"})
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

	user, err := gorm.G[models.User](db.DB).Where("email = ?", req.Email).First(c.Request.Context())
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

func ChangePasswordHandler(c *gin.Context) {
	claims, ok := auth.GetClaims(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authentication"})
		return
	}

	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "currentPassword and newPassword are required, and newPassword must be at least 8 characters"})
		return
	}

	user, err := gorm.G[models.User](db.DB).Where("id = ?", claims.Subject).First(c.Request.Context())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authentication"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	if user.PasswordHash == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot change password for managed user"})
		return
	}

	if !auth.CheckPassword(req.CurrentPassword, *user.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "current password is incorrect"})
		return
	}

	if auth.CheckPassword(req.NewPassword, *user.PasswordHash) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "new password must be different from current password"})
		return
	}

	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	user.PasswordHash = &hash
	user.PasswordChangeRequired = false

	if err := db.DB.WithContext(c.Request.Context()).Save(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	token, err := auth.GenerateUserToken(&user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	auth.SetAuthCookie(c, token)
	c.JSON(http.StatusOK, gin.H{"message": "password changed successfully"})
}

func LogoutHandler(c *gin.Context) {
	auth.ClearAuthCookie(c)
	c.JSON(http.StatusOK, gin.H{"message": "logged out successfully"})
}
