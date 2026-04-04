package routes

import (
	"net/http"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/oidc"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// OIDCAppURL is set during route registration from the server config.
var OIDCAppURL string

type providerInfo struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

type providersResponse struct {
	Providers        []providerInfo `json:"providers"`
	LocalAuthEnabled bool           `json:"localAuthEnabled"`
}

func ListProvidersHandler(c *gin.Context) {
	providers, err := gorm.G[models.OIDCProvider](db.DB).Where("enabled = ?", true).Find(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	infos := make([]providerInfo, 0, len(providers))
	for _, p := range providers {
		infos = append(infos, providerInfo{Id: p.Id, Name: p.Name})
	}

	c.JSON(http.StatusOK, providersResponse{
		Providers:        infos,
		LocalAuthEnabled: !LocalAuthDisabled,
	})
}

func OIDCAuthorizeHandler(c *gin.Context) {
	providerID := c.Param("id")

	provider, err := gorm.G[models.OIDCProvider](db.DB).Where("id = ? AND enabled = ?", providerID, true).First(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}

	authURL, encryptedState, err := oidc.StartAuth(c.Request.Context(), &provider, OIDCAppURL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start authentication"})
		return
	}

	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(oidc.StateCookieName(), encryptedState, int(10*time.Minute/time.Second), "/", "", true, true)

	c.Redirect(http.StatusFound, authURL)
}

func OIDCCallbackHandler(c *gin.Context) {
	providerID := c.Param("id")

	if errParam := c.Query("error"); errParam != "" {
		c.Redirect(http.StatusFound, "/login?error="+errParam)
		return
	}

	code := c.Query("code")
	stateParam := c.Query("state")
	if code == "" || stateParam == "" {
		c.Redirect(http.StatusFound, "/login?error=invalid_callback")
		return
	}

	encryptedState, err := c.Cookie(oidc.StateCookieName())
	if err != nil {
		c.Redirect(http.StatusFound, "/login?error=missing_state")
		return
	}

	// Clear state cookie immediately
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(oidc.StateCookieName(), "", -1, "/", "", true, true)

	provider, err := gorm.G[models.OIDCProvider](db.DB).Where("id = ? AND enabled = ?", providerID, true).First(c.Request.Context())
	if err != nil {
		c.Redirect(http.StatusFound, "/login?error=provider_not_found")
		return
	}

	oidcUser, err := oidc.HandleCallback(c.Request.Context(), &provider, OIDCAppURL, code, stateParam, encryptedState)
	if err != nil {
		c.Redirect(http.StatusFound, "/login?error=authentication_failed")
		return
	}

	// Find or create user by OIDC identity
	user, err := gorm.G[models.User](db.DB).Where("oidc_issuer = ? AND oidc_subject = ?", oidcUser.Issuer, oidcUser.Subject).First(c.Request.Context())
	if err != nil {
		// User doesn't exist — JIT provision
		user = models.User{
			Email:        oidcUser.Email,
			Name:         oidcUser.Name,
			AuthProvider: models.AuthProviderOIDC,
			Role:         models.UserRoleUser,
			OIDCSubject:  &oidcUser.Subject,
			OIDCIssuer:   &oidcUser.Issuer,
		}
		if err := gorm.G[models.User](db.DB).Create(c.Request.Context(), &user); err != nil {
			c.Redirect(http.StatusFound, "/login?error=account_creation_failed")
			return
		}
	} else {
		// Update name and email from OIDC claims on each login
		db.DB.WithContext(c.Request.Context()).Model(&models.User{}).Where("id = ?", user.Id).Updates(map[string]any{"name": oidcUser.Name, "email": oidcUser.Email}) //nolint:errcheck
	}

	token, err := auth.GenerateUserToken(&user)
	if err != nil {
		c.Redirect(http.StatusFound, "/login?error=token_generation_failed")
		return
	}

	auth.SetAuthCookie(c, token)
	c.Redirect(http.StatusFound, "/")
}
