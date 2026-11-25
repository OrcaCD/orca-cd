package routes

import (
	"context"
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
	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/OrcaCD/orca-cd/internal/hub/seclog"
	"github.com/OrcaCD/orca-cd/internal/hub/setupcode"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
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
	Name            string `json:"name" maxLength:"64" doc:"The name of the first admin, at least 3 characters. Only used with setupCode."`
	Email           string `json:"email" doc:"The email of the first admin. Only used with setupCode."`
	SetupCode       string `json:"setupCode" maxLength:"128" doc:"The setup code from the hub log, to create the first admin"`
	EnrollmentToken string `json:"enrollmentToken" maxLength:"128" doc:"The enrollment code an admin issued for an account"`
	PasskeyName     string `json:"passkeyName" maxLength:"64"`
}

type passkeyRegistrationEmail struct {
	Email string `binding:"omitempty,email"`
}

type ownPasskeyRequest struct {
	Name              string `json:"name" required:"true" minLength:"1" maxLength:"64"`
	VerificationToken string `json:"verificationToken" maxLength:"128" doc:"The proof from verifying an existing passkey for the add action"`
}

type passkeyVerificationRequest struct {
	Action    string `json:"action" required:"true" enum:"add,delete" doc:"The key-management action the proof allows"`
	PasskeyID string `json:"passkeyId" maxLength:"128" doc:"The passkey to delete. Required for delete, not allowed for add."`
}

type deletePasskeyBody struct {
	VerificationToken string `json:"verificationToken" doc:"The proof from verifying an existing passkey for deleting this one, at most 128 characters"`
}

// passkeyOptionsOutput holds the WebAuthn options the browser passes to
// navigator.credentials. Their challenge is single use, so they are never cached.
type passkeyOptionsOutput[T any] struct {
	CacheControl string `header:"Cache-Control"`
	Body         T
}

type passkeyCreationOptionsOutput = passkeyOptionsOutput[protocol.PublicKeyCredentialCreationOptions]
type passkeyRequestOptionsOutput = passkeyOptionsOutput[protocol.PublicKeyCredentialRequestOptions]

type passkeyVerificationOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         struct {
		VerificationToken string `json:"verificationToken" required:"true" doc:"A single-use proof for the verified action"`
	}
}

// webAuthnCredentialBody documents the body of the finish routes. go-webauthn
// parses it from the request itself, after the ceremony cookie was checked.
// It has no schema, which would make Huma read and validate the body first.
func webAuthnCredentialBody(description string) *huma.RequestBody {
	return &huma.RequestBody{
		Description: description + " At most 64 KiB.",
		Required:    true,
		Content:     map[string]*huma.MediaType{"application/json": {}},
	}
}

func checkPasskeysAvailable(setup bool) error {
	if DemoMode || (LocalAuthDisabled && !setup) {
		return huma.Error403Forbidden("local authentication is disabled")
	}
	if passkeyWebAuthn == nil {
		return huma.Error503ServiceUnavailable("passkeys require a valid HTTPS APP_URL or localhost")
	}
	return nil
}

func saveCeremony[T any](c *gin.Context, ceremony passkeys.Ceremony, options T) (*passkeyOptionsOutput[T], error) {
	id, err := passkeySessions.Put(ceremony)
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("too many pending passkey requests")
	}
	auth.SetPasskeyCookie(c, id, passkeys.CeremonyLifetime)
	return &passkeyOptionsOutput[T]{CacheControl: "no-store", Body: options}, nil
}

func takeCeremony(c *gin.Context, purpose string) (passkeys.Ceremony, error) {
	id, _ := auth.GetPasskeyCookie(c)
	auth.ClearPasskeyCookie(c)
	ceremony, ok := passkeySessions.Take(id, purpose)
	if !ok {
		seclog.Warn(c, seclog.TokenRejected, "reason", "invalid_passkey_ceremony")
		return ceremony, huma.Error400BadRequest("passkey request expired or already used, please try again")
	}
	return ceremony, nil
}

// BeginPasskeyRegistrationHandler authorizes either the initial administrator or
// an explicitly invited account. Neither path grants a session before verification.
var BeginPasskeyRegistrationHandler = restapi.Handler(huma.Operation{
	OperationID: "begin-passkey-registration",
	Method:      http.MethodPost,
	Path:        "/auth/passkeys/register/begin",
	Summary:     "Start registering an account",
	Description: "Creates the first admin with the setup code, or sets up an account with the enrollment code an admin issued. " +
		"Returns the WebAuthn creation options and sets a cookie for the ceremony. Rate limited.",
	Tags:     []string{tagAuth},
	Security: restapi.Public,
	Errors:   []int{http.StatusBadRequest, http.StatusForbidden, http.StatusServiceUnavailable},
}, func(ctx context.Context, input *struct{ Body passkeyRegistrationRequest }) (*passkeyCreationOptionsOutput, error) {
	c := restapi.GinContext(ctx)
	req := input.Body
	// Checked here instead of with format:"email", which would reject the
	// empty email of a registration with an enrollment token.
	if err := binding.Validator.ValidateStruct(passkeyRegistrationEmail{Email: req.Email}); err != nil {
		return nil, huma.Error400BadRequest("invalid registration request")
	}
	ceremony := passkeys.Ceremony{Purpose: "register", Name: strings.TrimSpace(req.PasskeyName)}
	var user passkeys.User
	if req.EnrollmentToken != "" {
		// An administrator created or recovered this account. The enrollment code
		// authorizes only the named user and is consumed when registration succeeds.
		if err := checkPasskeysAvailable(false); err != nil {
			return nil, err
		}
		hash := passkeys.TokenHash(req.EnrollmentToken)
		enrollment, err := gorm.G[models.PasskeyEnrollment](db.DB).Where("token_hash = ? AND expires_at > ?", hash, time.Now()).First(ctx)
		if err != nil {
			seclog.Warn(c, seclog.RegistrationFailed, "reason", "invalid_enrollment_code")
			return nil, huma.Error403Forbidden("invalid or expired enrollment code")
		}
		user, err = passkeys.LoadUser(ctx, db.DB, enrollment.UserId)
		if err != nil || !user.LocalAuthEnabled || user.TokenVersion != enrollment.TokenVersion {
			seclog.Warn(c, seclog.RegistrationFailed, "reason", "invalid_enrollment_code")
			return nil, huma.Error403Forbidden("invalid or expired enrollment code")
		}
		ceremony.EnrollmentHash = hash
	} else {
		// The first administrator proves access to the setup code from the hub log.
		// Later accounts must use an administrator-issued enrollment code above.
		if err := checkPasskeysAvailable(true); err != nil {
			return nil, err
		}
		if !setupcode.Verify(req.SetupCode) {
			seclog.Warn(c, seclog.SetupCodeFailed)
			return nil, huma.Error403Forbidden(errInvalidSetupCode)
		}
		count, err := gorm.G[models.User](db.DB).Count(ctx, "*")
		if err != nil {
			return nil, internalError()
		}
		if count != 0 {
			return nil, huma.Error403Forbidden("registration is disabled")
		}
		if utf8.RuneCountInString(strings.TrimSpace(req.Name)) < 3 || req.Email == "" {
			return nil, huma.Error400BadRequest("name (min 3 chars) and valid email are required")
		}
		user.User = models.User{Id: uuid.NewString(), Name: strings.TrimSpace(req.Name), Email: strings.ToLower(strings.TrimSpace(req.Email)), LocalAuthEnabled: true}
		ceremony.SetupCode = req.SetupCode
	}
	return beginRegistration(c, user, ceremony)
})

var BeginOwnPasskeyHandler = restapi.Handler(huma.Operation{
	OperationID: "begin-own-passkey",
	Method:      http.MethodPost,
	Path:        "/auth/passkeys/begin",
	Summary:     "Start adding a passkey",
	Description: "Needs a verification token for the add action from verifying an existing passkey. " +
		"Returns the WebAuthn creation options and sets a cookie for the ceremony. Rate limited.",
	Tags:     []string{tagProfile},
	Security: restapi.SessionOnly,
	Errors:   []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable},
}, func(ctx context.Context, input *struct{ Body ownPasskeyRequest }) (*passkeyCreationOptionsOutput, error) {
	if err := checkPasskeysAvailable(false); err != nil {
		return nil, err
	}
	c := restapi.GinContext(ctx)
	_, current, err := currentLocalUser(c)
	if err != nil {
		return nil, err
	}
	req := input.Body
	if strings.TrimSpace(req.Name) == "" {
		return nil, huma.Error400BadRequest("a passkey name is required")
	}
	if err := consumePasskeyVerification(c, current, req.VerificationToken, "add", ""); err != nil {
		return nil, err
	}
	user, err := passkeys.LoadUser(ctx, db.DB, current.Id)
	if err != nil {
		return nil, internalError()
	}
	if !user.LocalAuthEnabled || user.TokenVersion != current.TokenVersion {
		return nil, huma.Error403Forbidden("passkey account changed, please sign in again")
	}
	return beginRegistration(c, user, passkeys.Ceremony{Purpose: "add", Name: strings.TrimSpace(req.Name)})
})

// Verification grants one key-management operation, bound to the current account
// and session version. A stolen session cookie alone cannot issue this proof.
var BeginPasskeyVerificationHandler = restapi.Handler(huma.Operation{
	OperationID: "begin-passkey-verification",
	Method:      http.MethodPost,
	Path:        "/auth/passkeys/verify/begin",
	Summary:     "Start verifying a passkey",
	Description: "Adding or deleting a passkey needs a fresh verification with an existing one. " +
		"Returns the WebAuthn request options and sets a cookie for the ceremony. Rate limited.",
	Tags:     []string{tagProfile},
	Security: restapi.SessionOnly,
	Errors:   []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusServiceUnavailable},
}, func(ctx context.Context, input *struct{ Body passkeyVerificationRequest }) (*passkeyRequestOptionsOutput, error) {
	if err := checkPasskeysAvailable(false); err != nil {
		return nil, err
	}
	c := restapi.GinContext(ctx)
	_, current, err := currentLocalUser(c)
	if err != nil {
		return nil, err
	}
	req := input.Body
	if (req.Action == "delete") != (req.PasskeyID != "") {
		return nil, huma.Error400BadRequest("choose a key-management action and a target for deletion")
	}
	if req.Action == "delete" {
		if _, err := gorm.G[models.Passkey](db.DB).Select("id").Where("id = ? AND user_id = ?", req.PasskeyID, current.Id).First(ctx); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, huma.Error404NotFound("passkey not found")
			}
			return nil, internalError()
		}
	}
	user, err := passkeys.LoadUser(ctx, db.DB, current.Id)
	if err != nil {
		return nil, internalError()
	}
	if !user.LocalAuthEnabled || user.TokenVersion != current.TokenVersion {
		return nil, huma.Error403Forbidden("passkey account changed, please sign in again")
	}
	// An account signed in through SSO can have no passkey yet. The first one
	// needs an enrollment code, otherwise a stolen session cookie could add one.
	if len(user.Credentials) == 0 {
		return nil, huma.Error409Conflict("add your first passkey with an enrollment code")
	}
	options, session, err := passkeyWebAuthn.BeginLogin(user)
	if err != nil {
		return nil, huma.Error400BadRequest("could not start passkey verification")
	}
	return saveCeremony(c, passkeys.Ceremony{Purpose: "verify", User: user.User, Session: *session, Action: req.Action, PasskeyID: req.PasskeyID}, options.Response)
})

var FinishPasskeyVerificationHandler = restapi.Handler(huma.Operation{
	OperationID: "finish-passkey-verification",
	Method:      http.MethodPost,
	Path:        "/auth/passkeys/verify/finish",
	Summary:     "Finish verifying a passkey",
	Description: "Checks the assertion of an existing passkey and returns a single-use verification token " +
		"for the action the verification was started for. Rate limited.",
	Tags:        []string{tagProfile},
	Security:    restapi.SessionOnly,
	RequestBody: webAuthnCredentialBody("The credential JSON from navigator.credentials.get()."),
	Errors:      []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable},
}, func(ctx context.Context, _ *struct{}) (*passkeyVerificationOutput, error) {
	c := restapi.GinContext(ctx)
	ceremony, err := takeCeremony(c, "verify")
	if err != nil {
		return nil, err
	}
	if err := checkPasskeysAvailable(false); err != nil {
		return nil, err
	}
	_, current, err := currentLocalUser(c)
	if err != nil {
		return nil, err
	}
	if current.Id != ceremony.User.Id || current.TokenVersion != ceremony.User.TokenVersion {
		return nil, huma.Error403Forbidden("passkey account mismatch")
	}
	response, err := protocol.ParseCredentialRequestResponseBody(http.MaxBytesReader(c.Writer, c.Request.Body, maxPasskeyResponseBytes))
	if err != nil {
		seclog.Warn(c, seclog.LoginFailed, "reason", "passkey_verification_failed")
		return nil, huma.Error401Unauthorized("passkey verification failed")
	}
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
		return nil, huma.Error401Unauthorized("passkey verification failed")
	}
	token, err := passkeySessions.Put(passkeys.Ceremony{Purpose: "manage", User: ceremony.User, Action: ceremony.Action, PasskeyID: ceremony.PasskeyID})
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("too many pending passkey requests")
	}
	out := &passkeyVerificationOutput{CacheControl: "no-store"}
	out.Body.VerificationToken = token
	return out, nil
})

func consumePasskeyVerification(c *gin.Context, user *models.User, token, action, passkeyID string) error {
	proof, ok := passkeySessions.Take(token, "manage")
	if !ok || proof.User.Id != user.Id || proof.User.TokenVersion != user.TokenVersion || proof.Action != action || proof.PasskeyID != passkeyID {
		seclog.Warn(c, seclog.TokenRejected, "reason", "invalid_passkey_verification")
		return errPasskeyVerificationRequired()
	}
	return nil
}

func errPasskeyVerificationRequired() error {
	return huma.Error403Forbidden("verify an existing passkey before changing your keys")
}

func beginRegistration(c *gin.Context, user passkeys.User, ceremony passkeys.Ceremony) (*passkeyCreationOptionsOutput, error) {
	if len(user.Credentials) >= passkeys.MaxCredentials {
		return nil, huma.Error400BadRequest("passkey limit reached")
	}
	options, session, err := passkeyWebAuthn.BeginRegistration(user, webauthn.WithExclusions(webauthn.Credentials(user.Credentials).CredentialDescriptors()))
	if err != nil {
		return nil, huma.Error500InternalServerError("could not start passkey registration")
	}
	ceremony.User = user.User
	ceremony.Session = *session
	if ceremony.Name == "" {
		ceremony.Name = "Passkey"
	}
	return saveCeremony(c, ceremony, options.Response)
}

var FinishPasskeyRegistrationHandler = restapi.Handler(huma.Operation{
	OperationID:   "finish-passkey-registration",
	Method:        http.MethodPost,
	Path:          "/auth/passkeys/register/finish",
	DefaultStatus: http.StatusCreated,
	Summary:       "Finish registering an account",
	Description:   "Creates the account and its first passkey and sets the session cookie. Rate limited.",
	Tags:          []string{tagAuth},
	Security:      restapi.Public,
	RequestBody:   webAuthnCredentialBody("The credential JSON from navigator.credentials.create()."),
	Errors:        []int{http.StatusBadRequest, http.StatusForbidden, http.StatusServiceUnavailable},
}, func(ctx context.Context, _ *struct{}) (*restapi.MessageOutput, error) {
	return finishRegistration(ctx, "register")
})

var FinishOwnPasskeyHandler = restapi.Handler(huma.Operation{
	OperationID:   "finish-own-passkey",
	Method:        http.MethodPost,
	Path:          "/auth/passkeys/finish",
	DefaultStatus: http.StatusCreated,
	Summary:       "Finish adding a passkey",
	Description:   "Saves the new passkey of the signed-in user. Rate limited.",
	Tags:          []string{tagProfile},
	Security:      restapi.SessionOnly,
	RequestBody:   webAuthnCredentialBody("The credential JSON from navigator.credentials.create()."),
	Errors:        []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable},
}, func(ctx context.Context, _ *struct{}) (*restapi.MessageOutput, error) {
	return finishRegistration(ctx, "add")
})

func finishRegistration(ctx context.Context, purpose string) (*restapi.MessageOutput, error) {
	c := restapi.GinContext(ctx)
	ceremony, err := takeCeremony(c, purpose)
	if err != nil {
		return nil, err
	}
	setup := ceremony.SetupCode != ""
	if err := checkPasskeysAvailable(setup); err != nil {
		return nil, err
	}
	if purpose == "add" {
		_, user, err := currentLocalUser(c)
		if err != nil {
			return nil, err
		}
		if user.Id != ceremony.User.Id {
			return nil, huma.Error403Forbidden("passkey account mismatch")
		}
	}
	response, err := protocol.ParseCredentialCreationResponseBody(http.MaxBytesReader(c.Writer, c.Request.Body, maxPasskeyResponseBytes))
	if err != nil {
		seclog.Warn(c, seclog.RegistrationFailed, "reason", "invalid_passkey_response")
		return nil, huma.Error400BadRequest("passkey registration failed, please try again")
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
		return nil, huma.Error400BadRequest("passkey registration failed, please try again")
	}
	if setup {
		setupcode.Clear()
	}
	if purpose == "register" {
		if err := setLoginCookie(c, &user); err != nil {
			return nil, err
		}
		seclog.Info(c, seclog.RegistrationSuccess, "user_id", user.Id, "auth_method", "passkey")
	}
	utils.RecordAuditLog(c, "passkey-added", "passkey", addedCredentialID)
	return restapi.Message("passkey registered"), nil
}

var BeginPasskeyLoginHandler = restapi.Handler(huma.Operation{
	OperationID: "begin-passkey-login",
	Method:      http.MethodPost,
	Path:        "/auth/passkeys/login/begin",
	Summary:     "Start signing in with a passkey",
	Description: "Returns the WebAuthn request options for a discoverable passkey and sets a cookie for the ceremony. Rate limited.",
	Tags:        []string{tagAuth},
	Security:    restapi.Public,
	Errors:      []int{http.StatusForbidden, http.StatusServiceUnavailable},
}, func(ctx context.Context, _ *struct{}) (*passkeyRequestOptionsOutput, error) {
	c := restapi.GinContext(ctx)
	if err := checkPasskeysAvailable(false); err != nil {
		if LocalAuthDisabled || DemoMode {
			seclog.Warn(c, seclog.LoginFailed, "reason", "local_auth_disabled")
		}
		return nil, err
	}
	options, session, err := passkeyWebAuthn.BeginDiscoverableLogin()
	if err != nil {
		return nil, huma.Error500InternalServerError("could not start passkey login")
	}
	return saveCeremony(c, passkeys.Ceremony{Purpose: "login", Session: *session}, options.Response)
})

var FinishPasskeyLoginHandler = restapi.Handler(huma.Operation{
	OperationID: "finish-passkey-login",
	Method:      http.MethodPost,
	Path:        "/auth/passkeys/login/finish",
	Summary:     "Finish signing in with a passkey",
	Description: "Checks the assertion and sets the session cookie. Rate limited.",
	Tags:        []string{tagAuth},
	Security:    restapi.Public,
	RequestBody: webAuthnCredentialBody("The credential JSON from navigator.credentials.get()."),
	Errors:      []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable},
}, func(ctx context.Context, _ *struct{}) (*restapi.MessageOutput, error) {
	c := restapi.GinContext(ctx)
	ceremony, err := takeCeremony(c, "login")
	if err != nil {
		return nil, err
	}
	if err := checkPasskeysAvailable(false); err != nil {
		return nil, err
	}
	response, err := protocol.ParseCredentialRequestResponseBody(http.MaxBytesReader(c.Writer, c.Request.Body, maxPasskeyResponseBytes))
	if err != nil {
		seclog.Warn(c, seclog.LoginFailed, "reason", "invalid_passkey_response")
		return nil, huma.Error401Unauthorized("passkey authentication failed")
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
		return nil, huma.Error401Unauthorized("passkey authentication failed")
	}
	if err := setLoginCookie(c, &user); err != nil {
		return nil, err
	}
	seclog.Info(c, seclog.LoginSuccess, "user_id", user.Id, "auth_method", "passkey")
	return restapi.Message("login successful"), nil
})

// setLoginCookie signs the user in with a new session cookie.
func setLoginCookie(c *gin.Context, user *models.User) error {
	token, err := auth.GenerateUserToken(user)
	if err != nil {
		return internalError()
	}
	auth.SetAuthCookie(c, token)
	return nil
}

type passkeyResponse struct {
	Id         string     `json:"id" required:"true"`
	Name       string     `json:"name" required:"true"`
	CreatedAt  time.Time  `json:"createdAt" required:"true" format:"date-time"`
	LastUsedAt *time.Time `json:"lastUsedAt" required:"true" nullable:"true" format:"date-time"`
}

var ListPasskeysHandler = restapi.Handler(huma.Operation{
	OperationID: "list-passkeys",
	Method:      http.MethodGet,
	Path:        "/auth/passkeys",
	Summary:     "List your passkeys",
	Description: "Lists the passkeys of the signed-in user, oldest first.",
	Tags:        []string{tagProfile},
	Security:    restapi.SessionOnly,
	Errors:      []int{http.StatusUnauthorized, http.StatusForbidden},
}, func(ctx context.Context, _ *struct{}) (*struct{ Body []passkeyResponse }, error) {
	_, user, err := currentLocalUser(restapi.GinContext(ctx))
	if err != nil {
		return nil, err
	}
	keys, err := gorm.G[models.Passkey](db.DB).Select("id", "name", "created_at", "last_used_at").Where("user_id = ?", user.Id).Order("created_at").Find(ctx)
	if err != nil {
		return nil, internalError()
	}
	response := make([]passkeyResponse, 0, len(keys))
	for _, key := range keys {
		response = append(response, passkeyResponse{Id: key.Id, Name: key.Name, CreatedAt: key.CreatedAt, LastUsedAt: key.LastUsedAt})
	}
	return &struct{ Body []passkeyResponse }{Body: response}, nil
})

var errLastPasskey = errors.New("cannot delete the last passkey")

var DeletePasskeyHandler = restapi.Handler(huma.Operation{
	OperationID: "delete-passkey",
	Method:      http.MethodDelete,
	Path:        "/auth/passkeys/{id}",
	Summary:     "Delete a passkey",
	Description: "Needs a verification token for deleting this passkey from verifying an existing one. " +
		"The last passkey of a user can't be deleted.",
	Tags:     []string{tagProfile},
	Security: restapi.SessionOnly,
	Errors:   []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusServiceUnavailable},
}, func(ctx context.Context, input *struct {
	ID   string `path:"id" doc:"The ID of the passkey"`
	Body *deletePasskeyBody
}) (*restapi.MessageOutput, error) {
	if err := checkPasskeysAvailable(false); err != nil {
		return nil, err
	}
	c := restapi.GinContext(ctx)
	_, user, err := currentLocalUser(c)
	if err != nil {
		return nil, err
	}
	// A missing or invalid proof is refused like a wrong one
	if input.Body == nil || input.Body.VerificationToken == "" || len(input.Body.VerificationToken) > 128 {
		return nil, errPasskeyVerificationRequired()
	}
	if err := consumePasskeyVerification(c, user, input.Body.VerificationToken, "delete", input.ID); err != nil {
		return nil, err
	}
	err = db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := gorm.G[models.User](tx).Where("id = ?", user.Id).First(ctx)
		if err != nil {
			return err
		}
		if !current.LocalAuthEnabled || current.TokenVersion != user.TokenVersion {
			return gorm.ErrRecordNotFound
		}
		rows, err := gorm.G[models.Passkey](tx).Where("id = ? AND user_id = ?", input.ID, user.Id).Delete(ctx)
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
		return nil, huma.Error400BadRequest(errLastPasskey.Error())
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, huma.Error404NotFound("passkey not found")
	case err != nil:
		return nil, internalError()
	}
	utils.RecordAuditLog(c, "passkey-deleted", "passkey", input.ID)
	return restapi.Message("passkey deleted"), nil
})
