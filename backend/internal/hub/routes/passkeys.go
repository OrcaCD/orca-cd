package routes

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/passkeys"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/OrcaCD/orca-cd/internal/hub/seclog"
	"github.com/OrcaCD/orca-cd/internal/hub/setupcode"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var passkeyWebAuthn *webauthn.WebAuthn
var passkeySessions passkeys.Sessions

const ceremonyCookieName = auth.CeremonyCookieName

// WebAuthn responses need a tighter cap than the hub's general-purpose request
// limit, which also accommodates deployment and configuration payloads.
const maxPasskeyResponseBytes = 64 * 1024 // 64 KiB

func InitPasskeys(appURL string) error {
	wa, err := passkeys.New(appURL)
	passkeyWebAuthn = wa
	return err
}

type passkeyRegistrationRequest struct {
	Name            string `json:"name" binding:"max=64"`
	Email           string `json:"email" binding:"omitempty,email"`
	SetupCode       string `json:"setupCode" binding:"max=128"`
	EnrollmentToken string `json:"enrollmentToken" binding:"max=128"`
	PasskeyName     string `json:"passkeyName" binding:"max=64"`
}

func passkeysAvailable(c *gin.Context, setup bool) bool {
	if DemoMode || (LocalAuthDisabled && !setup) {
		c.JSON(http.StatusForbidden, gin.H{"error": "local authentication is disabled"})
		return false
	}
	if passkeyWebAuthn == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "passkeys require a valid HTTPS APP_URL or localhost"})
		return false
	}
	return true
}

func saveCeremony(c *gin.Context, ceremony passkeys.Ceremony, options any) {
	id, err := passkeySessions.Put(ceremony)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "too many pending passkey requests"})
		return
	}
	auth.SetPasskeyCookie(c, id, passkeys.CeremonyLifetime)
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, options)
}

func takeCeremony(c *gin.Context, purpose string) (passkeys.Ceremony, bool) {
	id, _ := auth.GetPasskeyCookie(c)
	auth.ClearPasskeyCookie(c)
	ceremony, ok := passkeySessions.Take(id, purpose)
	if !ok {
		seclog.Warn(c, seclog.TokenRejected, "reason", "invalid_passkey_ceremony")
		c.JSON(http.StatusBadRequest, gin.H{"error": "passkey request expired or already used, please try again"})
	}
	return ceremony, ok
}

// BeginPasskeyRegistrationHandler authorizes either the initial administrator or
// an explicitly invited account. Neither path grants a session before verification.
func BeginPasskeyRegistrationHandler(c *gin.Context) {
	var req passkeyRegistrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid registration request"})
		return
	}
	ctx := c.Request.Context()
	ceremony := passkeys.Ceremony{Purpose: "register", Name: strings.TrimSpace(req.PasskeyName)}
	var user passkeys.User
	if req.EnrollmentToken != "" {
		// An administrator created or recovered this account. The enrollment code
		// authorizes only the named user and is consumed when registration succeeds.
		if !passkeysAvailable(c, false) {
			return
		}
		hash := passkeys.TokenHash(req.EnrollmentToken)
		enrollment, err := gorm.G[models.PasskeyEnrollment](db.DB).Where("token_hash = ? AND expires_at > ?", hash, time.Now()).First(ctx)
		if err != nil {
			seclog.Warn(c, seclog.RegistrationFailed, "reason", "invalid_enrollment_code")
			c.JSON(http.StatusForbidden, gin.H{"error": "invalid or expired enrollment code"})
			return
		}
		user, err = passkeys.LoadUser(ctx, db.DB, enrollment.UserId)
		if err != nil || !user.LocalAuthEnabled || user.TokenVersion != enrollment.TokenVersion {
			seclog.Warn(c, seclog.RegistrationFailed, "reason", "invalid_enrollment_code")
			c.JSON(http.StatusForbidden, gin.H{"error": "invalid or expired enrollment code"})
			return
		}
		ceremony.EnrollmentHash = hash
	} else {
		// The first administrator proves access to the setup code from the hub log.
		// Later accounts must use an administrator-issued enrollment code above.
		if !passkeysAvailable(c, true) {
			return
		}
		if !setupcode.Verify(req.SetupCode) {
			seclog.Warn(c, seclog.SetupCodeFailed)
			c.JSON(http.StatusForbidden, gin.H{"error": errInvalidSetupCode})
			return
		}
		count, err := gorm.G[models.User](db.DB).Count(ctx, "*")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			return
		}
		if count != 0 {
			c.JSON(http.StatusForbidden, gin.H{"error": "registration is disabled"})
			return
		}
		if utf8.RuneCountInString(strings.TrimSpace(req.Name)) < 3 || req.Email == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name (min 3 chars) and valid email are required"})
			return
		}
		user.User = models.User{Id: uuid.NewString(), Name: strings.TrimSpace(req.Name), Email: strings.ToLower(strings.TrimSpace(req.Email)), LocalAuthEnabled: true}
		ceremony.SetupCode = req.SetupCode
	}
	beginRegistration(c, user, ceremony)
}

func BeginOwnPasskeyHandler(c *gin.Context) {
	if !passkeysAvailable(c, false) {
		return
	}
	_, current, ok := getCurrentLocalUser(c)
	if !ok {
		return
	}
	var req struct {
		Name              string `json:"name" binding:"required,max=64"`
		VerificationToken string `json:"verificationToken" binding:"max=128"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a passkey name is required"})
		return
	}
	if !consumePasskeyVerification(c, current, req.VerificationToken, "add", "") {
		return
	}
	user, err := passkeys.LoadUser(c.Request.Context(), db.DB, current.Id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if !user.LocalAuthEnabled || user.TokenVersion != current.TokenVersion {
		c.JSON(http.StatusForbidden, gin.H{"error": "passkey account changed, please sign in again"})
		return
	}
	beginRegistration(c, user, passkeys.Ceremony{Purpose: "add", Name: strings.TrimSpace(req.Name)})
}

// Verification grants one key-management operation, bound to the current account
// and session version. A stolen session cookie alone cannot issue this proof.
func BeginPasskeyVerificationHandler(c *gin.Context) {
	if !passkeysAvailable(c, false) {
		return
	}
	_, current, ok := getCurrentLocalUser(c)
	if !ok {
		return
	}
	var req struct {
		Action    string `json:"action" binding:"required,oneof=add delete"`
		PasskeyID string `json:"passkeyId" binding:"max=128"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || (req.Action == "delete") != (req.PasskeyID != "") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "choose a key-management action and a target for deletion"})
		return
	}
	if req.Action == "delete" {
		if _, err := gorm.G[models.Passkey](db.DB).Select("id").Where("id = ? AND user_id = ?", req.PasskeyID, current.Id).First(c.Request.Context()); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": "passkey not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			}
			return
		}
	}
	user, err := passkeys.LoadUser(c.Request.Context(), db.DB, current.Id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if !user.LocalAuthEnabled || user.TokenVersion != current.TokenVersion {
		c.JSON(http.StatusForbidden, gin.H{"error": "passkey account changed, please sign in again"})
		return
	}
	// An account signed in through SSO can have no passkey yet. The first one
	// needs an enrollment code, otherwise a stolen session cookie could add one.
	if len(user.Credentials) == 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "add your first passkey with an enrollment code"})
		return
	}
	options, session, err := passkeyWebAuthn.BeginLogin(user)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "could not start passkey verification"})
		return
	}
	saveCeremony(c, passkeys.Ceremony{Purpose: "verify", User: user.User, Session: *session, Action: req.Action, PasskeyID: req.PasskeyID}, options.Response)
}

func FinishPasskeyVerificationHandler(c *gin.Context) {
	ceremony, ok := takeCeremony(c, "verify")
	if !ok || !passkeysAvailable(c, false) {
		return
	}
	_, current, ok := getCurrentLocalUser(c)
	if !ok {
		return
	}
	if current.Id != ceremony.User.Id || current.TokenVersion != ceremony.User.TokenVersion {
		c.JSON(http.StatusForbidden, gin.H{"error": "passkey account mismatch"})
		return
	}
	response, err := protocol.ParseCredentialRequestResponseBody(http.MaxBytesReader(c.Writer, c.Request.Body, maxPasskeyResponseBytes))
	if err != nil {
		seclog.Warn(c, seclog.LoginFailed, "reason", "passkey_verification_failed")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "passkey verification failed"})
		return
	}
	ctx := c.Request.Context()
	err = db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := passkeys.LoadUser(ctx, tx, current.Id)
		if err != nil {
			return err
		}
		if !user.LocalAuthEnabled || user.TokenVersion != ceremony.User.TokenVersion {
			return gorm.ErrRecordNotFound
		}
		credential, err := passkeyWebAuthn.ValidateLogin(user, ceremony.Session, response)
		if err != nil {
			return err
		}
		if credential.Authenticator.CloneWarning {
			return errors.New("authenticator counter did not advance")
		}
		return passkeys.UpdateCredential(ctx, tx, user.Id, credential)
	})
	if err != nil {
		seclog.Warn(c, seclog.LoginFailed, "reason", "passkey_verification_failed")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "passkey verification failed"})
		return
	}
	token, err := passkeySessions.Put(passkeys.Ceremony{Purpose: "manage", User: ceremony.User, Action: ceremony.Action, PasskeyID: ceremony.PasskeyID})
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "too many pending passkey requests"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"verificationToken": token})
}

func consumePasskeyVerification(c *gin.Context, user *models.User, token, action, passkeyID string) bool {
	proof, ok := passkeySessions.Take(token, "manage")
	if !ok || proof.User.Id != user.Id || proof.User.TokenVersion != user.TokenVersion || proof.Action != action || proof.PasskeyID != passkeyID {
		seclog.Warn(c, seclog.TokenRejected, "reason", "invalid_passkey_verification")
		c.JSON(http.StatusForbidden, gin.H{"error": "verify an existing passkey before changing your keys"})
		return false
	}
	return true
}

func beginRegistration(c *gin.Context, user passkeys.User, ceremony passkeys.Ceremony) {
	if len(user.Credentials) >= passkeys.MaxCredentials {
		c.JSON(http.StatusBadRequest, gin.H{"error": "passkey limit reached"})
		return
	}
	options, session, err := passkeyWebAuthn.BeginRegistration(user, webauthn.WithExclusions(webauthn.Credentials(user.Credentials).CredentialDescriptors()))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not start passkey registration"})
		return
	}
	ceremony.User = user.User
	ceremony.Session = *session
	if ceremony.Name == "" {
		ceremony.Name = "Passkey"
	}
	saveCeremony(c, ceremony, options.Response)
}

func FinishPasskeyRegistrationHandler(c *gin.Context) { finishRegistration(c, "register") }
func FinishOwnPasskeyHandler(c *gin.Context)          { finishRegistration(c, "add") }

func finishRegistration(c *gin.Context, purpose string) {
	ceremony, ok := takeCeremony(c, purpose)
	if !ok {
		return
	}
	setup := ceremony.SetupCode != ""
	if !passkeysAvailable(c, setup) {
		return
	}
	if purpose == "add" {
		_, user, ok := getCurrentLocalUser(c)
		if !ok {
			return
		}
		if user.Id != ceremony.User.Id {
			c.JSON(http.StatusForbidden, gin.H{"error": "passkey account mismatch"})
			return
		}
	}
	ctx := c.Request.Context()
	response, err := protocol.ParseCredentialCreationResponseBody(http.MaxBytesReader(c.Writer, c.Request.Body, maxPasskeyResponseBytes))
	if err != nil {
		seclog.Warn(c, seclog.RegistrationFailed, "reason", "invalid_passkey_response")
		c.JSON(http.StatusBadRequest, gin.H{"error": "passkey registration failed, please try again"})
		return
	}
	var user models.User
	var addedCredentialID string
	err = db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var waUser passkeys.User
		if setup {
			if !setupcode.Verify(ceremony.SetupCode) {
				return errRegistrationDisabled
			}
			count, err := gorm.G[models.User](tx).Count(ctx, "*")
			if err != nil {
				return err
			}
			if count != 0 {
				return errRegistrationDisabled
			}
			waUser.User = ceremony.User
		} else {
			var err error
			waUser, err = passkeys.LoadUser(ctx, tx, ceremony.User.Id)
			if err != nil {
				return err
			}
			if !waUser.LocalAuthEnabled || waUser.TokenVersion != ceremony.User.TokenVersion {
				return gorm.ErrRecordNotFound
			}
			if ceremony.EnrollmentHash != "" {
				if err := passkeys.ConsumeEnrollment(ctx, tx, &waUser.User, ceremony.EnrollmentHash); err != nil {
					return err
				}
			}
		}
		credential, err := passkeyWebAuthn.CreateCredential(waUser, ceremony.Session, response)
		if err != nil {
			return err
		}
		user = waUser.User
		if setup {
			if err := gorm.G[models.User](tx).Create(ctx, &user); err != nil {
				return err
			}
			if err := rbac.AssignBuiltInRole(ctx, tx, user.Id, models.RoleBuiltInAdmin); err != nil {
				return err
			}
		}
		if err := passkeys.SaveCredential(ctx, tx, user.Id, ceremony.Name, credential); err != nil {
			return err
		}
		added, err := gorm.G[models.Passkey](tx).Select("id").Where("user_id = ? AND credential_id = ?", user.Id, passkeys.CredentialID(credential.ID)).First(ctx)
		addedCredentialID = added.Id
		return err
	})
	if err != nil {
		seclog.Warn(c, seclog.RegistrationFailed, "reason", "passkey_registration_rejected")
		c.JSON(http.StatusBadRequest, gin.H{"error": "passkey registration failed, please try again"})
		return
	}
	if setup {
		setupcode.Clear()
	}
	if purpose == "register" {
		if !setPasskeyLoginCookie(c, &user) {
			return
		}
		seclog.Info(c, seclog.RegistrationSuccess, "user_id", user.Id, "auth_method", "passkey")
	}
	utils.RecordAuditLog(c, "passkey-added", "passkey", addedCredentialID)
	c.JSON(http.StatusCreated, gin.H{"message": "passkey registered"})
}

func BeginPasskeyLoginHandler(c *gin.Context) {
	if !passkeysAvailable(c, false) {
		if LocalAuthDisabled || DemoMode {
			seclog.Warn(c, seclog.LoginFailed, "reason", "local_auth_disabled")
		}
		return
	}
	options, session, err := passkeyWebAuthn.BeginDiscoverableLogin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not start passkey login"})
		return
	}
	saveCeremony(c, passkeys.Ceremony{Purpose: "login", Session: *session}, options.Response)
}

func FinishPasskeyLoginHandler(c *gin.Context) {
	ceremony, ok := takeCeremony(c, "login")
	if !ok || !passkeysAvailable(c, false) {
		return
	}
	ctx := c.Request.Context()
	response, err := protocol.ParseCredentialRequestResponseBody(http.MaxBytesReader(c.Writer, c.Request.Body, maxPasskeyResponseBytes))
	if err != nil {
		seclog.Warn(c, seclog.LoginFailed, "reason", "invalid_passkey_response")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "passkey authentication failed"})
		return
	}
	var user models.User
	err = db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		waUser, credential, err := passkeyWebAuthn.ValidatePasskeyLogin(func(rawID, handle []byte) (webauthn.User, error) {
			key, err := gorm.G[models.Passkey](tx).Where("credential_id = ? AND user_id = ?", passkeys.CredentialID(rawID), string(handle)).First(ctx)
			if err != nil {
				return nil, err
			}
			u, err := passkeys.LoadUser(ctx, tx, key.UserId)
			if err != nil {
				return nil, err
			}
			if !u.LocalAuthEnabled {
				return nil, errors.New("local authentication is disabled for account")
			}
			return u, nil
		}, ceremony.Session, response)
		if err != nil {
			return err
		}
		if credential.Authenticator.CloneWarning {
			return errors.New("authenticator counter did not advance")
		}
		user = waUser.(passkeys.User).User
		return passkeys.UpdateCredential(ctx, tx, user.Id, credential)
	})
	if err != nil {
		seclog.Warn(c, seclog.LoginFailed, "reason", "invalid_passkey_assertion")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "passkey authentication failed"})
		return
	}
	if !setPasskeyLoginCookie(c, &user) {
		return
	}
	seclog.Info(c, seclog.LoginSuccess, "user_id", user.Id, "auth_method", "passkey")
	c.JSON(http.StatusOK, gin.H{"message": "login successful"})
}

func setPasskeyLoginCookie(c *gin.Context, user *models.User) bool {
	token, err := auth.GenerateUserToken(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return false
	}
	auth.SetAuthCookie(c, token)
	return true
}

type passkeyResponse struct {
	Id         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
}

func ListPasskeysHandler(c *gin.Context) {
	_, user, ok := getCurrentLocalUser(c)
	if !ok {
		return
	}
	keys, err := gorm.G[models.Passkey](db.DB).Select("id", "name", "created_at", "last_used_at").Where("user_id = ?", user.Id).Order("created_at").Find(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	response := make([]passkeyResponse, 0, len(keys))
	for _, key := range keys {
		response = append(response, passkeyResponse{Id: key.Id, Name: key.Name, CreatedAt: key.CreatedAt, LastUsedAt: key.LastUsedAt})
	}
	c.JSON(http.StatusOK, response)
}

var errLastPasskey = errors.New("cannot delete the last passkey")

func DeletePasskeyHandler(c *gin.Context) {
	if !passkeysAvailable(c, false) {
		return
	}
	_, user, ok := getCurrentLocalUser(c)
	if !ok {
		return
	}
	var req struct {
		VerificationToken string `json:"verificationToken" binding:"required,max=128"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "verify an existing passkey before changing your keys"})
		return
	}
	if !consumePasskeyVerification(c, user, req.VerificationToken, "delete", c.Param("id")) {
		return
	}
	ctx := c.Request.Context()
	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := gorm.G[models.User](tx).Where("id = ?", user.Id).First(ctx)
		if err != nil {
			return err
		}
		if !current.LocalAuthEnabled || current.TokenVersion != user.TokenVersion {
			return gorm.ErrRecordNotFound
		}
		rows, err := gorm.G[models.Passkey](tx).Where("id = ? AND user_id = ?", c.Param("id"), user.Id).Delete(ctx)
		if err != nil {
			return err
		}
		if rows == 0 {
			return gorm.ErrRecordNotFound
		}
		count, err := gorm.G[models.Passkey](tx).Where("user_id = ?", user.Id).Count(ctx, "*")
		if err != nil {
			return err
		}
		if count == 0 {
			return errLastPasskey
		}
		return nil
	})
	switch {
	case errors.Is(err, errLastPasskey):
		c.JSON(http.StatusBadRequest, gin.H{"error": errLastPasskey.Error()})
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "passkey not found"})
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	default:
		utils.RecordAuditLog(c, "passkey-deleted", "passkey", c.Param("id"))
		c.JSON(http.StatusOK, gin.H{"message": "passkey deleted"})
	}
}
