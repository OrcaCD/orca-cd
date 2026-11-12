package routes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/oidc"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/OrcaCD/orca-cd/internal/shared/logger"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var (
	errOIDCSignupDisabled        = errors.New("oidc signup disabled")
	errOIDCAccountLinkingFailed  = errors.New("oidc account linking failed")
	errOIDCAccountCreationFailed = errors.New("oidc account creation failed")
	errOIDCAccountUpdateFailed   = errors.New("oidc account update failed")
	errOIDCProviderEmailConflict = errors.New("oidc provider email conflict")
	errOIDCRoleSyncFailed        = errors.New("oidc role sync failed")
)

var oidcLog = logger.New("oidc", false)

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
		if errors.Is(err, oidc.ErrGroupsUnavailable) {
			oidcLog.Error().Err(err).Str("providerId", provider.Id).Msg("failed to read the groups of an OIDC user")
			c.Redirect(http.StatusFound, "/login?error=role_sync_failed")
			return
		}
		c.Redirect(http.StatusFound, "/login?error=authentication_failed")
		return
	}

	user, rolesChanged, redirectError := resolveOIDCUser(c.Request.Context(), &provider, oidcUser)
	if redirectError != "" {
		c.Redirect(http.StatusFound, "/login?error="+redirectError)
		return
	}
	if rolesChanged {
		utils.RecordAuditLog(c, "roles-synced", "user", user.Id)
		publishRolesChanged([]string{user.Id})
	}

	token, err := auth.GenerateUserTokenWithPicture(user, oidcUser.Picture)
	if err != nil {
		c.Redirect(http.StatusFound, "/login?error=token_generation_failed")
		return
	}

	auth.SetAuthCookie(c, token)
	c.Redirect(http.StatusFound, "/")
}

// resolveOIDCUser finds or creates the user of an OIDC login and updates the
// roles they get from their groups. Returns whether their roles changed.
func resolveOIDCUser(ctx context.Context, provider *models.OIDCProvider, oidcUser *oidc.OIDCUser) (*models.User, bool, string) {
	var user models.User
	rolesChanged := false

	txErr := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		created, err := linkOIDCUser(ctx, tx, provider, oidcUser, &user)
		if err != nil {
			return err
		}
		if rolesChanged, err = syncOIDCGroups(ctx, tx, provider, oidcUser, user.Id, created); err != nil {
			oidcLog.Error().Err(err).Str("userId", user.Id).Str("providerId", provider.Id).Msg("failed to sync roles from OIDC groups")
			return errOIDCRoleSyncFailed
		}
		return nil
	})

	if txErr == nil {
		return &user, rolesChanged, ""
	}

	switch {
	case errors.Is(txErr, errOIDCSignupDisabled):
		return nil, false, "signup_disabled"
	case errors.Is(txErr, errOIDCAccountCreationFailed):
		return nil, false, "account_creation_failed"
	case errors.Is(txErr, errOIDCAccountLinkingFailed):
		return nil, false, "account_linking_failed"
	case errors.Is(txErr, errOIDCAccountUpdateFailed):
		return nil, false, "account_update_failed"
	case errors.Is(txErr, errOIDCProviderEmailConflict):
		return nil, false, "provider_email_conflict"
	case errors.Is(txErr, errOIDCRoleSyncFailed):
		return nil, false, "role_sync_failed"
	default:
		return nil, false, "internal_error"
	}
}

// linkOIDCUser finds the user of an OIDC login, links it to an existing user
// by email or creates one. Returns whether the user was created.
func linkOIDCUser(ctx context.Context, tx *gorm.DB, provider *models.OIDCProvider, oidcUser *oidc.OIDCUser, user *models.User) (bool, error) {
	created := false
	identity, err := gorm.G[models.UserOIDCIdentity](tx).Where("provider_id = ? AND subject = ?", provider.Id, oidcUser.Subject).First(ctx)
	if err == nil {
		*user, err = gorm.G[models.User](tx).Where("id = ?", identity.UserId).First(ctx)
		if err != nil {
			return false, err
		}

		hasConflict, err := hasProviderEmailConflict(tx, provider.Id, oidcUser.Email, user.Id)
		if err != nil {
			return false, err
		}
		if hasConflict {
			return false, errOIDCProviderEmailConflict
		}

		if err := tx.Model(&models.User{}).Where("id = ?", user.Id).Updates(map[string]any{"name": oidcUser.Name, "email": oidcUser.Email, "email_unconfirmed": false}).Error; err != nil {
			return false, errOIDCAccountUpdateFailed
		}
		user.Name = oidcUser.Name
		user.Email = oidcUser.Email

		return false, nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}

	*user, err = gorm.G[models.User](tx).Where("email = ?", oidcUser.Email).First(ctx)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return false, err
		}

		if !provider.AutoSignup {
			return false, errOIDCSignupDisabled
		}

		*user = models.User{
			Email: oidcUser.Email,
			Name:  oidcUser.Name,
		}
		if err := gorm.G[models.User](tx).Create(ctx, user); err != nil {
			return false, errOIDCAccountCreationFailed
		}
		created = true
	} else {
		// Users can set any unused email on their own account, so linking by
		// email is only safe if an admin set it.
		if user.EmailUnconfirmed {
			oidcLog.Warn().Str("userId", user.Id).Str("providerId", provider.Id).Msg("refusing to link OIDC login to an account whose email was changed by its user")
			return false, errOIDCAccountLinkingFailed
		}
		if err := tx.Model(&models.User{}).Where("id = ?", user.Id).Update("name", oidcUser.Name).Error; err != nil {
			return false, errOIDCAccountLinkingFailed
		}
		user.Name = oidcUser.Name
	}

	hasConflict, err := hasProviderEmailConflict(tx, provider.Id, oidcUser.Email, user.Id)
	if err != nil {
		return false, err
	}
	if hasConflict {
		return false, errOIDCProviderEmailConflict
	}

	identity = models.UserOIDCIdentity{
		UserId:     user.Id,
		ProviderId: provider.Id,
		Subject:    oidcUser.Subject,
		Groups:     []string{},
	}

	if err := gorm.G[models.UserOIDCIdentity](tx).Create(ctx, &identity); err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(strings.ToLower(err.Error()), "unique constraint failed") {
			existing, findErr := gorm.G[models.UserOIDCIdentity](tx).Where("provider_id = ? AND subject = ?", provider.Id, oidcUser.Subject).First(ctx)
			if findErr == nil && existing.UserId == user.Id {
				return created, nil
			}
		}
		return false, errOIDCAccountLinkingFailed
	}

	return created, nil
}

// syncOIDCGroups stores the groups of the user at the provider and updates the
// roles they get from groups. New users without such roles get the default role.
func syncOIDCGroups(ctx context.Context, tx *gorm.DB, provider *models.OIDCProvider, oidcUser *oidc.OIDCUser, userID string, created bool) (bool, error) {
	groups := oidcUser.Groups
	if provider.GroupsClaim == "" || groups == nil {
		groups = []string{}
	}
	encoded, err := json.Marshal(groups)
	if err != nil {
		return false, err
	}
	if _, err := gorm.G[models.UserOIDCIdentity](tx).
		Where("user_id = ? AND provider_id = ?", userID, provider.Id).
		Update(ctx, "group_names", string(encoded)); err != nil {
		return false, err
	}

	changed, err := rbac.SyncOIDCRoles(ctx, tx, []string{userID})
	if err != nil {
		return false, err
	}
	if created && len(changed) == 0 {
		if err := rbac.AssignBuiltInRole(ctx, tx, userID, models.RoleBuiltInUser); err != nil {
			return false, err
		}
	}
	return len(changed) > 0, nil
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
