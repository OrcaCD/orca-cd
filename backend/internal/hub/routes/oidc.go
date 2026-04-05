package routes

import (
	"errors"
	"net/http"
	"net/url"
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

func OIDCAuthorizeHandler(c *gin.Context) {
	providerId := c.Param("id")

	provider, err := gorm.G[models.OIDCProvider](db.DB).Where("id = ? AND enabled = ?", providerId, true).First(c.Request.Context())
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
	providerId := c.Param("id")

	if errParam := c.Query("error"); errParam != "" {
		c.Redirect(http.StatusFound, "/login?error="+url.QueryEscape(errParam))
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

	provider, err := gorm.G[models.OIDCProvider](db.DB).Where("id = ? AND enabled = ?", providerId, true).First(c.Request.Context())
	if err != nil {
		c.Redirect(http.StatusFound, "/login?error=provider_not_found")
		return
	}

	oidcUser, err := oidc.HandleCallback(c.Request.Context(), &provider, OIDCAppURL, code, stateParam, encryptedState)
	if err != nil {
		c.Redirect(http.StatusFound, "/login?error=authentication_failed")
		return
	}

	// Find user by OIDC identity, then fall back to email, then JIT provision.
	user, err := gorm.G[models.User](db.DB).Where("oidc_issuer = ? AND oidc_subject = ?", oidcUser.Issuer, oidcUser.Subject).First(c.Request.Context())
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			c.Redirect(http.StatusFound, "/login?error=internal_error")
			return
		}
		// No user linked to this OIDC identity yet — check if one exists with the same email.
		user, err = gorm.G[models.User](db.DB).Where("email = ?", oidcUser.Email).First(c.Request.Context())
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			c.Redirect(http.StatusFound, "/login?error=internal_error")
			return
		}
		if err != nil {
			// Completely new user — JIT provision
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
			// Existing account found by email — link the OIDC identity to it.
			tx := db.DB.WithContext(c.Request.Context()).Model(&models.User{}).Where("id = ?", user.Id).Updates(map[string]any{
				"oidc_subject": oidcUser.Subject,
				"oidc_issuer":  oidcUser.Issuer,
				"name":         oidcUser.Name,
			})

			if tx.Error != nil {
				c.Redirect(http.StatusFound, "/login?error=account_linking_failed")
				return
			}
		}
	} else {
		// Known OIDC user — update name and email from claims.
		tx := db.DB.WithContext(c.Request.Context()).Model(&models.User{}).Where("id = ?", user.Id).Updates(map[string]any{"name": oidcUser.Name, "email": oidcUser.Email})
		if tx.Error != nil {
			c.Redirect(http.StatusFound, "/login?error=account_update_failed")
			return
		}
	}

	token, err := auth.GenerateUserToken(&user)
	if err != nil {
		c.Redirect(http.StatusFound, "/login?error=token_generation_failed")
		return
	}

	auth.SetAuthCookie(c, token)
	c.Redirect(http.StatusFound, "/")
}
