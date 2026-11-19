package routes

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/OrcaCD/orca-cd/internal/shared/httpclient"
	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/danielgtaylor/huma/v2"
	"gorm.io/gorm"
)

type createOIDCProviderRequest struct {
	Name                 string `json:"name" required:"true" minLength:"1" maxLength:"100"`
	IssuerURL            string `json:"issuerUrl" required:"true" minLength:"1" format:"uri" doc:"An http(s) URL that answers OIDC discovery"`
	ClientId             string `json:"clientId" required:"true" minLength:"1"`
	ClientSecret         string `json:"clientSecret" required:"true" minLength:"1"`
	Scopes               string `json:"scopes" doc:"Comma-separated scopes to request in addition to openid, email and profile"`
	Enabled              *bool  `json:"enabled" doc:"Defaults to true"`
	RequireVerifiedEmail *bool  `json:"requireVerifiedEmail" doc:"Whether users need a verified email. Defaults to true."`
	AutoSignup           *bool  `json:"autoSignup" doc:"Whether unknown users get an account when they sign in. Defaults to true."`
	// GroupsClaim enables syncing roles from groups, which is off by default.
	GroupsClaim *string `json:"groupsClaim" maxLength:"255" doc:"The ID token claim with the groups of the user, which give roles. Empty turns it off."`
}

type updateOIDCProviderRequest struct {
	Name                 string  `json:"name" required:"true" minLength:"1" maxLength:"100"`
	IssuerURL            string  `json:"issuerUrl" required:"true" minLength:"1" format:"uri" doc:"An http(s) URL that answers OIDC discovery"`
	ClientId             string  `json:"clientId" required:"true" minLength:"1"`
	ClientSecret         *string `json:"clientSecret" doc:"Leave it out or empty to keep the current secret"`
	Scopes               string  `json:"scopes" doc:"Comma-separated scopes to request in addition to openid, email and profile"`
	Enabled              *bool   `json:"enabled" doc:"Leave it out to keep the current value"`
	RequireVerifiedEmail *bool   `json:"requireVerifiedEmail" doc:"Leave it out to keep the current value"`
	AutoSignup           *bool   `json:"autoSignup" doc:"Leave it out to keep the current value"`
	GroupsClaim          *string `json:"groupsClaim" maxLength:"255" doc:"Leave it out to keep the current value, empty turns syncing roles from groups off"`
}

type oidcProviderResponse struct {
	Id                   string `json:"id" required:"true"`
	Name                 string `json:"name" required:"true"`
	IssuerURL            string `json:"issuerUrl" required:"true"`
	ClientId             string `json:"clientId" required:"true"`
	Scopes               string `json:"scopes" required:"true"`
	Enabled              bool   `json:"enabled" required:"true"`
	RequireVerifiedEmail bool   `json:"requireVerifiedEmail" required:"true"`
	AutoSignup           bool   `json:"autoSignup" required:"true"`
	GroupsClaim          string `json:"groupsClaim" required:"true" doc:"The claim roles are synced from, empty when that is off"`
	CallbackURL          string `json:"callbackUrl" required:"true" doc:"The redirect URL to register at the provider"`
	CreatedAt            string `json:"createdAt" required:"true" format:"date-time"`
	UpdatedAt            string `json:"updatedAt" required:"true" format:"date-time"`
}

type oidcProviderOutput struct {
	Body oidcProviderResponse
}

// isHTTPURL is the check of Gin's http_url validator, which the issuer URL had
// to pass. The uri format also accepts other schemes.
func isHTTPURL(value string) bool {
	u, err := url.Parse(strings.ToLower(value))
	if err != nil || u.Host == "" {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// oidcDiscoveryClient is the HTTP client used to validate issuer URLs.
// Replaced with an unrestricted client in tests.
var oidcDiscoveryClient = httpclient.Default

// validateOIDCIssuer attempts OIDC discovery using the SSRF-safe HTTP client.
func validateOIDCIssuer(ctx context.Context, issuerURL string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ctx = gooidc.ClientContext(ctx, oidcDiscoveryClient)
	_, err := gooidc.NewProvider(ctx, issuerURL)
	return err
}

func toOIDCProviderResponse(p *models.OIDCProvider) oidcProviderResponse {
	return oidcProviderResponse{
		Id:                   p.Id,
		Name:                 p.Name,
		IssuerURL:            p.IssuerURL,
		ClientId:             p.ClientId,
		Scopes:               p.Scopes,
		Enabled:              p.Enabled,
		RequireVerifiedEmail: p.RequireVerifiedEmail,
		AutoSignup:           p.AutoSignup,
		GroupsClaim:          p.GroupsClaim,
		CallbackURL:          fmt.Sprintf("%s/api/v1/auth/oidc/%s/callback", OIDCAppURL, p.Id),
		CreatedAt:            p.CreatedAt.Format(time.RFC3339),
		UpdatedAt:            p.UpdatedAt.Format(time.RFC3339),
	}
}

var AdminListOIDCProvidersHandler = restapi.Handler(huma.Operation{
	OperationID: "admin-list-oidc-providers",
	Method:      http.MethodGet,
	Path:        "/admin/oidc-providers",
	Summary:     "List OIDC providers",
	Description: "Lists every OIDC provider, including disabled ones. Client secrets are never returned. " + requiresAdmin,
	Tags:        []string{tagAdmin},
	Security:    restapi.Authenticated,
}, func(ctx context.Context, _ *struct{}) (*struct{ Body []oidcProviderResponse }, error) {
	providers, err := gorm.G[models.OIDCProvider](db.DB).Find(ctx)
	if err != nil {
		return nil, internalError()
	}

	result := make([]oidcProviderResponse, 0, len(providers))
	for _, p := range providers {
		result = append(result, toOIDCProviderResponse(&p))
	}

	return &struct{ Body []oidcProviderResponse }{Body: result}, nil
})

var AdminGetOIDCProviderHandler = restapi.Handler(huma.Operation{
	OperationID: "admin-get-oidc-provider",
	Method:      http.MethodGet,
	Path:        "/admin/oidc-providers/{id}",
	Summary:     "Get an OIDC provider",
	Description: "The client secret is never returned. " + requiresAdmin,
	Tags:        []string{tagAdmin},
	Security:    restapi.Authenticated,
	Errors:      []int{http.StatusNotFound},
}, func(ctx context.Context, input *idInput) (*oidcProviderOutput, error) {
	provider, err := gorm.G[models.OIDCProvider](db.DB).Where("id = ?", input.ID).First(ctx)
	if err != nil {
		return nil, huma.Error404NotFound("provider not found")
	}

	return &oidcProviderOutput{Body: toOIDCProviderResponse(&provider)}, nil
})

var AdminCreateOIDCProviderHandler = restapi.Handler(huma.Operation{
	OperationID:   "admin-create-oidc-provider",
	Method:        http.MethodPost,
	Path:          "/admin/oidc-providers",
	DefaultStatus: http.StatusCreated,
	Summary:       "Create an OIDC provider",
	Description:   "The issuer URL must answer OIDC discovery. " + requiresAdmin,
	Tags:          []string{tagAdmin},
	Security:      restapi.Authenticated,
	Errors:        []int{http.StatusBadRequest},
}, func(ctx context.Context, input *struct{ Body createOIDCProviderRequest }) (*oidcProviderOutput, error) {
	req := input.Body
	if !isHTTPURL(req.IssuerURL) {
		return nil, huma.Error400BadRequest("invalid request: name, issuerUrl, clientId, and clientSecret are required and must be valid")
	}

	// Validate issuer by attempting OIDC discovery
	if err := validateOIDCIssuer(ctx, req.IssuerURL); err != nil {
		return nil, huma.Error400BadRequest("failed to discover OIDC provider at the given issuer URL")
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	requireVerifiedEmail := true
	if req.RequireVerifiedEmail != nil {
		requireVerifiedEmail = *req.RequireVerifiedEmail
	}

	autoSignup := true
	if req.AutoSignup != nil {
		autoSignup = *req.AutoSignup
	}

	provider := models.OIDCProvider{
		Name:                 req.Name,
		IssuerURL:            req.IssuerURL,
		ClientId:             req.ClientId,
		ClientSecret:         crypto.EncryptedString(req.ClientSecret),
		Scopes:               req.Scopes,
		Enabled:              enabled,
		RequireVerifiedEmail: requireVerifiedEmail,
		AutoSignup:           autoSignup,
	}
	if req.GroupsClaim != nil {
		provider.GroupsClaim = strings.TrimSpace(*req.GroupsClaim)
	}

	if err := db.DB.WithContext(ctx).Select("*").Create(&provider).Error; err != nil {
		return nil, internalError()
	}

	utils.RecordAuditLog(restapi.GinContext(ctx), "created", "oidc-provider", provider.Id)
	sse.PublishUpdate(sse.TopicAdminOIDCProvider)

	return &oidcProviderOutput{Body: toOIDCProviderResponse(&provider)}, nil
})

var AdminUpdateOIDCProviderHandler = restapi.Handler(huma.Operation{
	OperationID: "admin-update-oidc-provider",
	Method:      http.MethodPut,
	Path:        "/admin/oidc-providers/{id}",
	Summary:     "Update an OIDC provider",
	Description: "A changed issuer URL must answer OIDC discovery. Disabling the provider signs out its users, " +
		"and changing its groups claim updates the roles users get from groups. " + requiresAdmin,
	Tags:     []string{tagAdmin},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusBadRequest, http.StatusNotFound},
}, func(ctx context.Context, input *struct {
	ID   string `path:"id" doc:"The ID of the OIDC provider"`
	Body updateOIDCProviderRequest
}) (*oidcProviderOutput, error) {
	id, req := input.ID, input.Body
	if !isHTTPURL(req.IssuerURL) {
		return nil, huma.Error400BadRequest("invalid request: name, issuerUrl, and clientId are required and must be valid")
	}

	existing, err := gorm.G[models.OIDCProvider](db.DB).Where("id = ?", id).First(ctx)
	if err != nil {
		return nil, huma.Error404NotFound("provider not found")
	}

	// Validate issuer if it changed
	if req.IssuerURL != existing.IssuerURL {
		if err := validateOIDCIssuer(ctx, req.IssuerURL); err != nil {
			return nil, huma.Error400BadRequest("failed to discover OIDC provider at the given issuer URL")
		}
	}

	wasEnabled := existing.Enabled
	previousGroupsClaim := existing.GroupsClaim
	existing.Name = req.Name
	existing.IssuerURL = req.IssuerURL
	existing.ClientId = req.ClientId
	existing.Scopes = req.Scopes
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	if req.RequireVerifiedEmail != nil {
		existing.RequireVerifiedEmail = *req.RequireVerifiedEmail
	}
	if req.AutoSignup != nil {
		existing.AutoSignup = *req.AutoSignup
	}
	if req.GroupsClaim != nil {
		existing.GroupsClaim = strings.TrimSpace(*req.GroupsClaim)
	}
	if req.ClientSecret != nil && *req.ClientSecret != "" {
		existing.ClientSecret = crypto.EncryptedString(*req.ClientSecret)
	}

	var rolesChanged []string
	err = db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&existing).Error; err != nil {
			return err
		}
		// Groups read from another claim no longer apply. Users get the groups
		// of the new claim on their next login.
		if previousGroupsClaim != existing.GroupsClaim {
			if _, err := gorm.G[models.UserOIDCIdentity](tx).Where("provider_id = ?", id).
				Update(ctx, "group_names", gorm.Expr("'[]'")); err != nil {
				return err
			}
		}
		// Roles from the groups of a disabled provider, or one that stopped
		// syncing them, are removed
		if wasEnabled != existing.Enabled || previousGroupsClaim != existing.GroupsClaim {
			if rolesChanged, err = syncAllOIDCRoles(ctx, tx); err != nil {
				return err
			}
		}
		if wasEnabled && !existing.Enabled {
			return revokeOIDCProviderSessions(ctx, tx, id)
		}
		return nil
	})
	if err != nil {
		return nil, internalError()
	}

	utils.RecordAuditLog(restapi.GinContext(ctx), "updated", "oidc-provider", id)
	sse.PublishUpdate(sse.TopicAdminOIDCProvider)
	publishRolesChanged(rolesChanged)

	return &oidcProviderOutput{Body: toOIDCProviderResponse(&existing)}, nil
})

var AdminDeleteOIDCProviderHandler = restapi.Handler(huma.Operation{
	OperationID: "admin-delete-oidc-provider",
	Method:      http.MethodDelete,
	Path:        "/admin/oidc-providers/{id}",
	Summary:     "Delete an OIDC provider",
	Description: "Signs out the users linked to the provider and removes the roles they got from its groups. " + requiresAdmin,
	Tags:        []string{tagAdmin},
	Security:    restapi.Authenticated,
	Errors:      []int{http.StatusNotFound},
}, func(ctx context.Context, input *idInput) (*restapi.MessageOutput, error) {
	id := input.ID

	var deleted int
	var rolesChanged []string
	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := revokeOIDCProviderSessions(ctx, tx, id); err != nil {
			return err
		}
		if _, err := gorm.G[models.RoleOIDCGroup](tx).Where("provider_id = ?", id).Delete(ctx); err != nil {
			return err
		}
		var err error
		if deleted, err = gorm.G[models.OIDCProvider](tx).Where("id = ?", id).Delete(ctx); err != nil {
			return err
		}
		rolesChanged, err = syncAllOIDCRoles(ctx, tx)
		return err
	})
	if err != nil {
		return nil, internalError()
	}
	if deleted == 0 {
		return nil, huma.Error404NotFound("provider not found")
	}

	utils.RecordAuditLog(restapi.GinContext(ctx), "deleted", "oidc-provider", id)
	sse.PublishUpdate(sse.TopicAdminOIDCProvider)
	publishRolesChanged(rolesChanged)

	return restapi.Message("provider deleted"), nil
})

// revokeOIDCProviderSessions signs out every user linked to the provider, so
// sessions issued through a disabled or deleted provider stop working.
func revokeOIDCProviderSessions(ctx context.Context, tx *gorm.DB, providerID string) error {
	linkedUsers := tx.Model(&models.UserOIDCIdentity{}).Select("user_id").Where("provider_id = ?", providerID)
	_, err := gorm.G[models.User](tx).Where("id IN (?)", linkedUsers).Update(ctx, "token_version", gorm.Expr("token_version + 1"))
	return err
}
