package routes

import (
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/OrcaCD/orca-cd/internal/hub/setupcode"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var errRegistrationDisabled = errors.New("registration disabled")

const errInvalidSetupCode = "invalid setup code"

// LocalAuthDisabled is set by the server config to gate local auth endpoints.
var LocalAuthDisabled bool

// DemoMode is set by the server config and exposed to the frontend.
var DemoMode bool

type verifySetupCodeRequest struct {
	SetupCode string `json:"setupCode" binding:"max=128"`
}

type updateOwnProfileRequest struct {
	Name  string `json:"name" binding:"required,min=3,max=64"`
	Email string `json:"email" binding:"required,email"`
}

type providerInfo struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

type setupResponse struct {
	NeedsSetup       bool           `json:"needsSetup"`
	Providers        []providerInfo `json:"providers"`
	LocalAuthEnabled bool           `json:"localAuthEnabled"`
	Demo             bool           `json:"demo"`
}

type profileResponse struct {
	Id          string                                           `json:"id"`
	Name        string                                           `json:"name"`
	Email       string                                           `json:"email"`
	Picture     string                                           `json:"picture,omitempty"`
	IsAdmin     bool                                             `json:"isAdmin"`
	Roles       []userRoleResponse                               `json:"roles"`
	Permissions map[models.PermissionResource]rbac.ResourceGrant `json:"permissions"`
	IsLocal     bool                                             `json:"isLocal"`
	Demo        bool                                             `json:"demo"`
}

func ProfileHandler(c *gin.Context) {
	claims, user, ok := getCurrentUser(c)
	if !ok {
		return
	}

	writeProfile(c, claims, user)
}

// writeProfile responds with the profile of the current user, including what they are allowed to do.
func writeProfile(c *gin.Context, claims *auth.UserClaims, user *models.User) {
	perms, err := rbac.For(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	roles := make([]userRoleResponse, 0, len(perms.Roles()))
	for _, role := range perms.Roles() {
		roles = append(roles, userRoleResponse{Id: role.Id, Name: role.Name, BuiltIn: role.BuiltIn, Sources: role.Sources})
	}
	slices.SortFunc(roles, compareUserRoles)

	c.JSON(http.StatusOK, profileResponse{
		Id:          user.Id,
		Name:        user.Name,
		Email:       user.Email,
		Picture:     profilePictureURL(claims.Picture),
		IsAdmin:     perms.IsAdmin(),
		Roles:       roles,
		Permissions: perms.Grants(),
		IsLocal:     user.LocalAuthEnabled,
		Demo:        DemoMode,
	})
}

// getCurrentUser returns the token claims and the user loaded by the auth middleware.
func getCurrentUser(c *gin.Context) (*auth.UserClaims, *models.User, bool) {
	claims, hasClaims := auth.GetClaims(c)
	user, hasUser := auth.GetUser(c)
	if !hasClaims || !hasUser {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authentication"})
		return nil, nil, false
	}
	return claims, user, true
}

func getCurrentLocalUser(c *gin.Context) (*auth.UserClaims, *models.User, bool) {
	claims, user, ok := getCurrentUser(c)
	if !ok {
		return nil, nil, false
	}

	if !user.LocalAuthEnabled {
		c.JSON(http.StatusForbidden, gin.H{"error": "this operation is not available for sso users"})
		return nil, nil, false
	}

	return claims, user, true
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

	if err := db.DB.WithContext(c.Request.Context()).Model(&models.User{}).Where("id = ?", user.Id).Updates(map[string]any{"name": req.Name, "email": req.Email, "email_unconfirmed": user.EmailUnconfirmed || req.Email != user.Email}).Error; err != nil {
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
	writeProfile(c, claims, user)
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
		Demo:             DemoMode,
	})
}

func VerifySetupCodeHandler(c *gin.Context) {
	var req verifySetupCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if !setupcode.Verify(req.SetupCode) {
		c.JSON(http.StatusForbidden, gin.H{"error": errInvalidSetupCode})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "setup code accepted"})
}

func LogoutHandler(c *gin.Context) {
	auth.ClearAuthCookie(c)
	c.JSON(http.StatusOK, gin.H{"message": "logged out successfully"})
}

// LogoutAllHandler invalidates every token issued to the current user, including the current one.
func LogoutAllHandler(c *gin.Context) {
	_, user, ok := getCurrentUser(c)
	if !ok {
		return
	}

	rows, err := gorm.G[models.User](db.DB).Where("id = ?", user.Id).Update(c.Request.Context(), "token_version", gorm.Expr("token_version + 1"))
	if err != nil || rows == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	utils.RecordAuditLog(c, "revoked-sessions", "user", user.Id)

	auth.ClearAuthCookie(c)
	c.JSON(http.StatusOK, gin.H{"message": "logged out everywhere"})
}

// DemoLoginHandler grants the deliberately public, read-only demo session.
func DemoLoginHandler(c *gin.Context) {
	if !DemoMode {
		c.JSON(http.StatusForbidden, gin.H{"error": "demo mode is disabled"})
		return
	}
	user, err := gorm.G[models.User](db.DB).Where("id = ?", db.DemoUserID).First(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if !setPasskeyLoginCookie(c, &user) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "login successful"})
}
