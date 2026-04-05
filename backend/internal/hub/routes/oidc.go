package routes

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/oidc"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var (
	errOIDCSignupDisabled        = errors.New("oidc signup disabled")
	errOIDCAccountLinkingFailed  = errors.New("oidc account linking failed")
	errOIDCAccountCreationFailed = errors.New("oidc account creation failed")
	errOIDCAccountUpdateFailed   = errors.New("oidc account update failed")
	errOIDCProviderEmailConflict = errors.New("oidc provider email conflict")
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
		if errors.Is(err, oidc.ErrEmailNotVerified) {
			c.Redirect(http.StatusFound, "/login?error=email_not_verified")
			return
		}
		c.Redirect(http.StatusFound, "/login?error=authentication_failed")
		return
	}

	user, redirectError := resolveOIDCUser(c.Request.Context(), &provider, oidcUser)
	if redirectError != "" {
		c.Redirect(http.StatusFound, "/login?error="+redirectError)
		return
	}

	token, err := auth.GenerateUserToken(&user)
	if err != nil {
		c.Redirect(http.StatusFound, "/login?error=token_generation_failed")
		return
	}

	auth.SetAuthCookie(c, token)
	c.Redirect(http.StatusFound, "/")
}

func resolveOIDCUser(ctx context.Context, provider *models.OIDCProvider, oidcUser *oidc.OIDCUser) (models.User, string) {
	var user models.User

	txErr := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		identity, err := gorm.G[models.UserOIDCIdentity](tx).Where("issuer = ? AND subject = ?", oidcUser.Issuer, oidcUser.Subject).First(ctx)
		if err == nil {
			user, err = gorm.G[models.User](tx).Where("id = ?", identity.UserId).First(ctx)
			if err != nil {
				return err
			}

			hasConflict, err := hasProviderEmailConflict(tx, provider.Id, oidcUser.Email, user.Id)
			if err != nil {
				return err
			}
			if hasConflict {
				return errOIDCProviderEmailConflict
			}

			if err := tx.Model(&models.User{}).Where("id = ?", user.Id).Updates(map[string]any{"name": oidcUser.Name, "email": oidcUser.Email}).Error; err != nil {
				return errOIDCAccountUpdateFailed
			}
			user.Name = oidcUser.Name
			user.Email = oidcUser.Email

			return nil
		}

		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		user, err = gorm.G[models.User](tx).Where("email = ?", oidcUser.Email).First(ctx)
		if err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}

			if !provider.AutoSignup {
				return errOIDCSignupDisabled
			}

			user = models.User{
				Email: oidcUser.Email,
				Name:  oidcUser.Name,
				Role:  models.UserRoleUser,
			}
			if err := gorm.G[models.User](tx).Create(ctx, &user); err != nil {
				return errOIDCAccountCreationFailed
			}
		} else {
			if err := tx.Model(&models.User{}).Where("id = ?", user.Id).Update("name", oidcUser.Name).Error; err != nil {
				return errOIDCAccountLinkingFailed
			}
			user.Name = oidcUser.Name
		}

		hasConflict, err := hasProviderEmailConflict(tx, provider.Id, oidcUser.Email, user.Id)
		if err != nil {
			return err
		}
		if hasConflict {
			return errOIDCProviderEmailConflict
		}

		providerId := provider.Id
		identity = models.UserOIDCIdentity{
			UserId:     user.Id,
			ProviderId: &providerId,
			Issuer:     oidcUser.Issuer,
			Subject:    oidcUser.Subject,
		}

		if err := gorm.G[models.UserOIDCIdentity](tx).Create(ctx, &identity); err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(strings.ToLower(err.Error()), "unique constraint failed") {
				existing, findErr := gorm.G[models.UserOIDCIdentity](tx).Where("issuer = ? AND subject = ?", oidcUser.Issuer, oidcUser.Subject).First(ctx)
				if findErr == nil && existing.UserId == user.Id {
					return nil
				}
			}
			return errOIDCAccountLinkingFailed
		}

		return nil
	})

	if txErr == nil {
		return user, ""
	}

	switch {
	case errors.Is(txErr, errOIDCSignupDisabled):
		return models.User{}, "signup_disabled"
	case errors.Is(txErr, errOIDCAccountCreationFailed):
		return models.User{}, "account_creation_failed"
	case errors.Is(txErr, errOIDCAccountLinkingFailed):
		return models.User{}, "account_linking_failed"
	case errors.Is(txErr, errOIDCAccountUpdateFailed):
		return models.User{}, "account_update_failed"
	case errors.Is(txErr, errOIDCProviderEmailConflict):
		return models.User{}, "provider_email_conflict"
	default:
		return models.User{}, "internal_error"
	}
}

func hasProviderEmailConflict(tx *gorm.DB, providerId string, email string, currentUserId string) (bool, error) {
	query := tx.Table("user_oidc_identities AS uoi").
		Joins("JOIN users AS u ON u.id = uoi.user_id").
		Where("uoi.provider_id = ? AND LOWER(u.email) = LOWER(?)", providerId, email)

	if currentUserId != "" {
		query = query.Where("uoi.user_id <> ?", currentUserId)
	}

	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}

	return count > 0, nil
}
