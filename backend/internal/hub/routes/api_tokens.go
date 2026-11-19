package routes

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/OrcaCD/orca-cd/internal/hub/apitoken"
	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	maxAPITokenNameLength = 64
	maxAPITokensPerUser   = 50
)

type createAPITokenRequest struct {
	Name      string                 `json:"name" required:"true" minLength:"1" doc:"Unique per user, 1 to 64 characters"`
	Scopes    []models.APITokenScope `json:"scopes" required:"true" doc:"What the token may do, e.g. application:deploy or admin. The owner needs the matching permission for each."`
	ExpiresAt time.Time              `json:"expiresAt" required:"true" doc:"At most one year in the future"`
}

type apiTokenOwnerResponse struct {
	Id    string `json:"id" required:"true"`
	Name  string `json:"name" required:"true"`
	Email string `json:"email" required:"true"`
}

type apiTokenResponse struct {
	Id         string                 `json:"id" required:"true"`
	Name       string                 `json:"name" required:"true"`
	Prefix     string                 `json:"prefix" required:"true" doc:"The start of the token, to recognize it"`
	Scopes     []models.APITokenScope `json:"scopes" required:"true"`
	ExpiresAt  string                 `json:"expiresAt" required:"true" format:"date-time"`
	LastUsedAt *string                `json:"lastUsedAt" required:"true" nullable:"true" format:"date-time"`
	CreatedAt  string                 `json:"createdAt" required:"true" format:"date-time"`
	// Owner is only set in the admin list.
	Owner *apiTokenOwnerResponse `json:"owner,omitempty" doc:"Only set in the admin list"`
}

type createAPITokenResponse struct {
	apiTokenResponse
	// Token is the plain token. It is only returned when the token is created.
	Token string `json:"token" required:"true" doc:"The token to authenticate with. Only returned once."`
}

var ListAPITokensHandler = restapi.Handler(huma.Operation{
	OperationID: "list-api-tokens",
	Method:      http.MethodGet,
	Path:        "/api-tokens",
	Summary:     "List your API tokens",
	Description: "Lists the API tokens of the user, newest first.",
	Tags:        []string{tagAPITokens},
	Security:    restapi.SessionOnly,
}, func(ctx context.Context, _ *struct{}) (*struct{ Body []apiTokenResponse }, error) {
	user, ok := auth.GetUser(restapi.GinContext(ctx))
	if !ok {
		return nil, huma.Error401Unauthorized("missing authentication")
	}

	tokens, err := gorm.G[models.APIToken](db.DB).
		Where("user_id = ?", user.Id).
		Order("created_at DESC, id DESC").
		Find(ctx)
	if err != nil {
		return nil, internalError()
	}

	response := make([]apiTokenResponse, 0, len(tokens))
	for i := range tokens {
		response = append(response, toAPITokenResponse(&tokens[i]))
	}
	return &struct{ Body []apiTokenResponse }{Body: response}, nil
})

var CreateAPITokenHandler = restapi.Handler(huma.Operation{
	OperationID:   "create-api-token",
	Method:        http.MethodPost,
	Path:          "/api-tokens",
	DefaultStatus: http.StatusCreated,
	Summary:       "Create an API token",
	Description: fmt.Sprintf("Returns the token, which is only shown once. A token can't do more than its owner, "+
		"and a user can have at most %d tokens. ", maxAPITokensPerUser) +
		requires(models.ResourceAPIToken, models.ActionCreate),
	Tags:     []string{tagAPITokens},
	Security: restapi.SessionOnly,
	Errors:   []int{http.StatusBadRequest, http.StatusForbidden, http.StatusConflict},
}, func(ctx context.Context, input *struct{ Body createAPITokenRequest }) (*struct{ Body createAPITokenResponse }, error) {
	c := restapi.GinContext(ctx)
	user, ok := auth.GetUser(c)
	if !ok {
		return nil, huma.Error401Unauthorized("missing authentication")
	}

	req := input.Body
	name := strings.TrimSpace(req.Name)
	if name == "" || utf8.RuneCountInString(name) > maxAPITokenNameLength {
		return nil, huma.Error400BadRequest("name must be between 1 and 64 characters")
	}

	scopes, err := normalizeAPITokenScopes(req.Scopes)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	perms, err := permissionsFor(c)
	if err != nil {
		return nil, err
	}
	if scope, ok := ungrantedAPITokenScope(perms, scopes); ok {
		return nil, huma.Error403Forbidden("you don't have the permission for scope " + string(scope))
	}

	now := time.Now()
	if err := validateAPITokenExpiry(req.ExpiresAt, now); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}

	count, err := gorm.G[models.APIToken](db.DB).Where("user_id = ?", user.Id).Count(ctx, "*")
	if err != nil {
		return nil, internalError()
	}
	if count >= maxAPITokensPerUser {
		return nil, huma.Error409Conflict(fmt.Sprintf("you can't have more than %d api tokens, revoke one first", maxAPITokensPerUser))
	}

	generated := apitoken.Generate()
	token := models.APIToken{
		UserId:    user.Id,
		Name:      name,
		TokenHash: generated.Hash,
		Prefix:    generated.Prefix,
		Scopes:    scopes,
		ExpiresAt: req.ExpiresAt.UTC(),
	}
	if err := gorm.G[models.APIToken](db.DB).Create(ctx, &token); err != nil {
		if isUniqueConstraintError(err) {
			return nil, huma.Error409Conflict("a token with this name already exists")
		}
		return nil, internalError()
	}

	utils.RecordAuditLog(c, "created", "api-token", token.Id)
	sse.PublishUpdate(sse.TopicAPITokensOf(user.Id), sse.TopicAdminAPITokens)

	return &struct{ Body createAPITokenResponse }{Body: createAPITokenResponse{
		apiTokenResponse: toAPITokenResponse(&token),
		Token:            generated.Plain,
	}}, nil
})

var RevokeAPITokenHandler = restapi.Handler(huma.Operation{
	OperationID: "revoke-api-token",
	Method:      http.MethodDelete,
	Path:        "/api-tokens/{id}",
	Summary:     "Revoke one of your API tokens",
	Description: "Deletes the token, which stops working immediately.",
	Tags:        []string{tagAPITokens},
	Security:    restapi.SessionOnly,
	Errors:      []int{http.StatusNotFound},
}, func(ctx context.Context, input *idInput) (*restapi.MessageOutput, error) {
	c := restapi.GinContext(ctx)
	user, ok := auth.GetUser(c)
	if !ok {
		return nil, huma.Error401Unauthorized("missing authentication")
	}

	return revokeAPIToken(c, gorm.G[models.APIToken](db.DB).Where("id = ? AND user_id = ?", input.ID, user.Id))
})

var AdminListAPITokensHandler = restapi.Handler(huma.Operation{
	OperationID: "admin-list-api-tokens",
	Method:      http.MethodGet,
	Path:        "/admin/api-tokens",
	Summary:     "List the API tokens of all users",
	Description: "Lists every API token with its owner, newest first. Needs an admin.",
	Tags:        []string{tagAdmin},
	Security:    restapi.SessionOnly,
}, func(ctx context.Context, _ *struct{}) (*struct{ Body []apiTokenResponse }, error) {
	tokens, err := gorm.G[models.APIToken](db.DB).
		Preload("User", nil).
		Order("created_at DESC, id DESC").
		Find(ctx)
	if err != nil {
		return nil, internalError()
	}

	response := make([]apiTokenResponse, 0, len(tokens))
	for i := range tokens {
		item := toAPITokenResponse(&tokens[i])
		if owner := tokens[i].User; owner != nil {
			item.Owner = &apiTokenOwnerResponse{Id: owner.Id, Name: owner.Name, Email: owner.Email}
		}
		response = append(response, item)
	}
	return &struct{ Body []apiTokenResponse }{Body: response}, nil
})

var AdminRevokeAPITokenHandler = restapi.Handler(huma.Operation{
	OperationID: "admin-revoke-api-token",
	Method:      http.MethodDelete,
	Path:        "/admin/api-tokens/{id}",
	Summary:     "Revoke the API token of any user",
	Description: "Deletes the token, which stops working immediately. Needs an admin.",
	Tags:        []string{tagAdmin},
	Security:    restapi.SessionOnly,
	Errors:      []int{http.StatusNotFound},
}, func(ctx context.Context, input *idInput) (*restapi.MessageOutput, error) {
	return revokeAPIToken(restapi.GinContext(ctx), gorm.G[models.APIToken](db.DB).Where("id = ?", input.ID))
})

func revokeAPIToken(c *gin.Context, query gorm.ChainInterface[models.APIToken]) (*restapi.MessageOutput, error) {
	ctx := c.Request.Context()
	token, err := query.First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("api token not found")
		}
		return nil, internalError()
	}

	deleted, err := gorm.G[models.APIToken](db.DB).Where("id = ?", token.Id).Delete(ctx)
	if err != nil {
		return nil, internalError()
	}
	if deleted == 0 {
		return nil, huma.Error404NotFound("api token not found")
	}

	// The token is deleted, so the entry points at its owner to keep that known.
	utils.RecordAuditLog(c, "revoked-api-token", "user", token.UserId)
	sse.PublishUpdate(sse.TopicAPITokensOf(token.UserId), sse.TopicAdminAPITokens)

	return restapi.Message("api token revoked"), nil
}

// normalizeAPITokenScopes rejects unknown scopes and returns the rest without
// duplicates, in the order of models.AllAPITokenScopes.
func normalizeAPITokenScopes(requested []models.APITokenScope) ([]models.APITokenScope, error) {
	if len(requested) == 0 {
		return nil, errors.New("at least one scope is required")
	}
	for _, scope := range requested {
		if !models.IsValidAPITokenScope(scope) {
			return nil, errors.New("invalid scope: " + string(scope))
		}
	}

	scopes := make([]models.APITokenScope, 0, len(requested))
	for _, scope := range models.AllAPITokenScopes() {
		if slices.Contains(requested, scope) {
			scopes = append(scopes, scope)
		}
	}
	return scopes, nil
}

// ungrantedAPITokenScope returns the first scope the user is not allowed to give
// a token, as a token can never do more than its owner. A permission on single
// resources is enough, the token is then limited to those when it is used.
func ungrantedAPITokenScope(perms *rbac.Permissions, scopes []models.APITokenScope) (models.APITokenScope, bool) {
	for _, scope := range scopes {
		if scope == models.APITokenScopeAdmin {
			if !perms.IsAdmin() {
				return scope, true
			}
			continue
		}
		resource, action, _ := scope.Permission()
		if !perms.CanAny(resource, action) {
			return scope, true
		}
	}
	return "", false
}

// maxAPITokenExpiry is a year and a day from now, so a date picker that offers "one
// year" still works when the browser's clock or time zone is ahead of the hub.
func maxAPITokenExpiry(now time.Time) time.Time {
	return now.AddDate(1, 0, 1)
}

func validateAPITokenExpiry(expiresAt, now time.Time) error {
	if !expiresAt.After(now) {
		return errors.New("expiresAt must be in the future")
	}
	if expiresAt.After(maxAPITokenExpiry(now)) {
		return errors.New("expiresAt must be at most one year in the future")
	}
	return nil
}

func toAPITokenResponse(token *models.APIToken) apiTokenResponse {
	var lastUsedAt *string
	if token.LastUsedAt != nil {
		formatted := token.LastUsedAt.Format(time.RFC3339)
		lastUsedAt = &formatted
	}

	scopes := token.Scopes
	if scopes == nil {
		scopes = []models.APITokenScope{}
	}

	return apiTokenResponse{
		Id:         token.Id,
		Name:       token.Name,
		Prefix:     token.Prefix,
		Scopes:     scopes,
		ExpiresAt:  token.ExpiresAt.Format(time.RFC3339),
		LastUsedAt: lastUsedAt,
		CreatedAt:  token.CreatedAt.Format(time.RFC3339),
	}
}
