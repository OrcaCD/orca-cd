package routes

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/OrcaCD/orca-cd/internal/hub/seclog"
	"github.com/OrcaCD/orca-cd/internal/hub/setupcode"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/danielgtaylor/huma/v2"
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
	SetupCode string `json:"setupCode" maxLength:"128" doc:"The setup code printed in the hub log on first start"`
}

type updateOwnProfileRequest struct {
	Name  string `json:"name" required:"true" minLength:"3" maxLength:"64"`
	Email string `json:"email" required:"true" format:"email"`
}

type providerInfo struct {
	Id   string `json:"id" required:"true"`
	Name string `json:"name" required:"true"`
}

type setupResponse struct {
	NeedsSetup       bool           `json:"needsSetup" required:"true" doc:"True until the first user exists"`
	Providers        []providerInfo `json:"providers" required:"true" doc:"The enabled OIDC providers users can sign in with"`
	LocalAuthEnabled bool           `json:"localAuthEnabled" required:"true" doc:"Whether users can sign in with passkeys"`
	Demo             bool           `json:"demo" required:"true" doc:"Whether the hub runs as a read-only demo"`
}

type profileResponse struct {
	Id          string                                           `json:"id" required:"true"`
	Name        string                                           `json:"name" required:"true"`
	Email       string                                           `json:"email" required:"true"`
	Picture     string                                           `json:"picture,omitempty" doc:"The URL of the profile picture, if the OIDC provider has one"`
	IsAdmin     bool                                             `json:"isAdmin" required:"true"`
	Roles       []userRoleResponse                               `json:"roles" required:"true"`
	Permissions map[models.PermissionResource]rbac.ResourceGrant `json:"permissions" required:"true" doc:"The effective permissions per resource type"`
	IsLocal     bool                                             `json:"isLocal" required:"true" doc:"Whether the user signs in with passkeys rather than OIDC"`
	Demo        bool                                             `json:"demo" required:"true" doc:"Whether the hub runs as a read-only demo"`
}

type profileOutput struct {
	Body profileResponse
}

var ProfileHandler = restapi.Handler(huma.Operation{
	OperationID: "get-profile",
	Method:      http.MethodGet,
	Path:        "/auth/profile",
	Summary:     "Get your profile",
	Description: "Returns the signed-in user, including their roles and what they are allowed to do.",
	Tags:        []string{tagProfile},
	Security:    restapi.SessionOnly,
	Errors:      []int{http.StatusUnauthorized},
}, func(ctx context.Context, _ *struct{}) (*profileOutput, error) {
	c := restapi.GinContext(ctx)
	claims, user, err := currentUser(c)
	if err != nil {
		return nil, err
	}

	return profileFor(c, claims, user)
})

// profileFor returns the profile of the current user, including what they are allowed to do.
func profileFor(c *gin.Context, claims *auth.UserClaims, user *models.User) (*profileOutput, error) {
	perms, err := rbac.For(c)
	if err != nil {
		return nil, internalError()
	}
	roles := make([]userRoleResponse, 0, len(perms.Roles()))
	for _, role := range perms.Roles() {
		roles = append(roles, userRoleResponse{Id: role.Id, Name: role.Name, BuiltIn: role.BuiltIn, Sources: role.Sources})
	}
	slices.SortFunc(roles, compareUserRoles)

	return &profileOutput{Body: profileResponse{
		Id:          user.Id,
		Name:        user.Name,
		Email:       user.Email,
		Picture:     profilePictureURL(claims.Picture),
		IsAdmin:     perms.IsAdmin(),
		Roles:       roles,
		Permissions: perms.Grants(),
		IsLocal:     user.LocalAuthEnabled,
		Demo:        DemoMode,
	}}, nil
}

// currentUser returns the token claims and the user loaded by the auth middleware.
func currentUser(c *gin.Context) (*auth.UserClaims, *models.User, error) {
	claims, hasClaims := auth.GetClaims(c)
	user, hasUser := auth.GetUser(c)
	if !hasClaims || !hasUser {
		return nil, nil, huma.Error401Unauthorized("missing authentication")
	}
	return claims, user, nil
}

func currentLocalUser(c *gin.Context) (*auth.UserClaims, *models.User, error) {
	claims, user, err := currentUser(c)
	if err != nil {
		return nil, nil, err
	}

	if !user.LocalAuthEnabled {
		return nil, nil, huma.Error403Forbidden("this operation is not available for sso users")
	}

	return claims, user, nil
}

var UpdateOwnProfileHandler = restapi.Handler(huma.Operation{
	OperationID: "update-profile",
	Method:      http.MethodPut,
	Path:        "/auth/profile",
	Summary:     "Update your profile",
	Description: "Changes the name and email of a user who signs in with passkeys and renews the session cookie. " +
		"A changed email is marked as unconfirmed. Users from an OIDC provider can't change their profile.",
	Tags:     []string{tagProfile},
	Security: restapi.SessionOnly,
	Errors:   []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict},
}, func(ctx context.Context, input *struct{ Body updateOwnProfileRequest }) (*profileOutput, error) {
	c := restapi.GinContext(ctx)
	req := input.Body
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

	claims, user, err := currentLocalUser(c)
	if err != nil {
		return nil, err
	}

	if err := db.DB.WithContext(ctx).Model(&models.User{}).Where("id = ?", user.Id).Updates(map[string]any{"name": req.Name, "email": req.Email, "email_unconfirmed": user.EmailUnconfirmed || req.Email != user.Email}).Error; err != nil {
		if isUniqueConstraintError(err) {
			return nil, huma.Error409Conflict("a user with this email already exists")
		}
		return nil, internalError()
	}
	user.Name = req.Name
	user.Email = req.Email

	token, err := auth.GenerateUserTokenWithPicture(user, claims.Picture)
	if err != nil {
		return nil, internalError()
	}

	auth.SetAuthCookie(c, token)
	return profileFor(c, claims, user)
})

var SetupHandler = restapi.Handler(huma.Operation{
	OperationID: "get-auth-setup",
	Method:      http.MethodGet,
	Path:        "/auth/setup",
	Summary:     "Get the sign-in options",
	Description: "Tells the login page whether the hub still needs its first user and how users can sign in.",
	Tags:        []string{tagAuth},
	Security:    restapi.Public,
}, func(ctx context.Context, _ *struct{}) (*struct{ Body setupResponse }, error) {
	count, err := gorm.G[models.User](db.DB).Count(ctx, "*")
	if err != nil {
		return nil, internalError()
	}

	providers, err := gorm.G[models.OIDCProvider](db.DB).Where("enabled = ?", true).Find(ctx)
	if err != nil {
		return nil, internalError()
	}

	infos := make([]providerInfo, 0, len(providers))
	for _, p := range providers {
		infos = append(infos, providerInfo{Id: p.Id, Name: p.Name})
	}

	return &struct{ Body setupResponse }{Body: setupResponse{
		NeedsSetup:       count == 0,
		Providers:        infos,
		LocalAuthEnabled: !LocalAuthDisabled,
		Demo:             DemoMode,
	}}, nil
})

var VerifySetupCodeHandler = restapi.Handler(huma.Operation{
	OperationID: "verify-setup-code",
	Method:      http.MethodPost,
	Path:        "/auth/setup/verify",
	Summary:     "Check the setup code",
	Description: "Checks the setup code from the hub log before the first user registers. Rate limited.",
	Tags:        []string{tagAuth},
	Security:    restapi.Public,
	Errors:      []int{http.StatusForbidden},
}, func(ctx context.Context, input *struct{ Body verifySetupCodeRequest }) (*restapi.MessageOutput, error) {
	if !setupcode.Verify(input.Body.SetupCode) {
		seclog.Warn(restapi.GinContext(ctx), seclog.SetupCodeFailed)
		return nil, huma.Error403Forbidden(errInvalidSetupCode)
	}

	return restapi.Message("setup code accepted"), nil
})

var LogoutHandler = restapi.Handler(huma.Operation{
	OperationID: "logout",
	Method:      http.MethodPost,
	Path:        "/auth/logout",
	Summary:     "Sign out",
	Description: "Clears the session cookie of this browser.",
	Tags:        []string{tagAuth},
	Security:    restapi.SessionOnly,
}, func(ctx context.Context, _ *struct{}) (*restapi.MessageOutput, error) {
	auth.ClearAuthCookie(restapi.GinContext(ctx))
	return restapi.Message("logged out successfully"), nil
})

// LogoutAllHandler invalidates every token issued to the current user, including the current one.
var LogoutAllHandler = restapi.Handler(huma.Operation{
	OperationID: "logout-all",
	Method:      http.MethodPost,
	Path:        "/auth/logout-all",
	Summary:     "Sign out everywhere",
	Description: "Invalidates every session of the user, including this one.",
	Tags:        []string{tagAuth},
	Security:    restapi.SessionOnly,
	Errors:      []int{http.StatusUnauthorized},
}, func(ctx context.Context, _ *struct{}) (*restapi.MessageOutput, error) {
	c := restapi.GinContext(ctx)
	_, user, err := currentUser(c)
	if err != nil {
		return nil, err
	}

	rows, err := gorm.G[models.User](db.DB).Where("id = ?", user.Id).Update(ctx, "token_version", gorm.Expr("token_version + 1"))
	if err != nil || rows == 0 {
		return nil, internalError()
	}

	utils.RecordAuditLog(c, "revoked-sessions", "user", user.Id)

	auth.ClearAuthCookie(c)
	return restapi.Message("logged out everywhere"), nil
})

// DemoLoginHandler grants the deliberately public, read-only demo session.
var DemoLoginHandler = restapi.Handler(huma.Operation{
	OperationID: "demo-login",
	Method:      http.MethodPost,
	Path:        "/auth/demo",
	Summary:     "Sign in to the demo",
	Description: "Sets a session cookie for the read-only demo user. Only available when the hub runs as a demo. Rate limited.",
	Tags:        []string{tagAuth},
	Security:    restapi.Public,
	Errors:      []int{http.StatusForbidden},
}, func(ctx context.Context, _ *struct{}) (*restapi.MessageOutput, error) {
	if !DemoMode {
		return nil, huma.Error403Forbidden("demo mode is disabled")
	}
	user, err := gorm.G[models.User](db.DB).Where("id = ?", db.DemoUserID).First(ctx)
	if err != nil {
		return nil, internalError()
	}
	if err := setLoginCookie(restapi.GinContext(ctx), &user); err != nil {
		return nil, err
	}
	return restapi.Message("login successful"), nil
})
