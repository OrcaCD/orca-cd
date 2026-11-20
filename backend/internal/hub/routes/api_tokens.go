package routes

import (
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
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	maxAPITokenNameLength = 64
	maxAPITokensPerUser   = 50
)

type createAPITokenRequest struct {
	Name      string                 `json:"name" binding:"required"`
	Scopes    []models.APITokenScope `json:"scopes" binding:"required"`
	ExpiresAt *time.Time             `json:"expiresAt" binding:"required"`
}

type apiTokenOwnerResponse struct {
	Id    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type apiTokenResponse struct {
	Id         string                 `json:"id"`
	Name       string                 `json:"name"`
	Prefix     string                 `json:"prefix"`
	Scopes     []models.APITokenScope `json:"scopes"`
	ExpiresAt  string                 `json:"expiresAt"`
	LastUsedAt *string                `json:"lastUsedAt"`
	CreatedAt  string                 `json:"createdAt"`
	// OAuth tells whether an MCP client got the token by asking the user. Its
	// prefix changes whenever the client refreshes it.
	OAuth bool `json:"oauth"`
	// Owner is only set in the admin list.
	Owner *apiTokenOwnerResponse `json:"owner,omitempty"`
}

type createAPITokenResponse struct {
	apiTokenResponse
	// Token is the plain token. It is only returned when the token is created.
	Token string `json:"token"`
}

func ListAPITokensHandler(c *gin.Context) {
	user, ok := auth.GetUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authentication"})
		return
	}

	tokens, err := gorm.G[models.APIToken](db.DB).
		Where("user_id = ?", user.Id).
		Order("created_at DESC, id DESC").
		Find(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	response := make([]apiTokenResponse, 0, len(tokens))
	for i := range tokens {
		response = append(response, toAPITokenResponse(&tokens[i]))
	}
	c.JSON(http.StatusOK, response)
}

func CreateAPITokenHandler(c *gin.Context) {
	user, ok := auth.GetUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authentication"})
		return
	}

	var req createAPITokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: name, scopes and expiresAt are required"})
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" || utf8.RuneCountInString(name) > maxAPITokenNameLength {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must be between 1 and 64 characters"})
		return
	}

	scopes, err := normalizeAPITokenScopes(req.Scopes)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	perms, ok := currentPermissions(c)
	if !ok {
		return
	}
	if scope, ok := ungrantedAPITokenScope(perms, scopes); ok {
		respondForbidden(c, "you don't have the permission for scope "+string(scope))
		return
	}

	now := time.Now()
	if err := validateAPITokenExpiry(*req.ExpiresAt, now); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	existing, err := gorm.G[models.APIToken](db.DB).Where("user_id = ?", user.Id).Find(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	// Ended OAuth grants are deleted when a client gets a new one, until then
	// they don't count.
	if kept, _ := replacedAPITokens(existing, ""); len(kept) >= maxAPITokensPerUser {
		c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("you can't have more than %d api tokens, revoke one first", maxAPITokensPerUser)})
		return
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
			c.JSON(http.StatusConflict, gin.H{"error": "a token with this name already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	utils.RecordAuditLog(c, "created", "api-token", token.Id)
	sse.PublishUpdate(sse.TopicAPITokensOf(user.Id), sse.TopicAdminAPITokens)

	c.JSON(http.StatusCreated, createAPITokenResponse{
		apiTokenResponse: toAPITokenResponse(&token),
		Token:            generated.Plain,
	})
}

func RevokeAPITokenHandler(c *gin.Context) {
	user, ok := auth.GetUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authentication"})
		return
	}

	revokeAPIToken(c, gorm.G[models.APIToken](db.DB).Where("id = ? AND user_id = ?", c.Param("id"), user.Id))
}

func AdminListAPITokensHandler(c *gin.Context) {
	tokens, err := gorm.G[models.APIToken](db.DB).
		Preload("User", nil).
		Order("created_at DESC, id DESC").
		Find(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	response := make([]apiTokenResponse, 0, len(tokens))
	for i := range tokens {
		item := toAPITokenResponse(&tokens[i])
		if owner := tokens[i].User; owner != nil {
			item.Owner = &apiTokenOwnerResponse{Id: owner.Id, Name: owner.Name, Email: owner.Email}
		}
		response = append(response, item)
	}
	c.JSON(http.StatusOK, response)
}

func AdminRevokeAPITokenHandler(c *gin.Context) {
	revokeAPIToken(c, gorm.G[models.APIToken](db.DB).Where("id = ?", c.Param("id")))
}

func revokeAPIToken(c *gin.Context, query gorm.ChainInterface[models.APIToken]) {
	ctx := c.Request.Context()
	token, err := query.First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "api token not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}

	deleted, err := gorm.G[models.APIToken](db.DB).Where("id = ?", token.Id).Delete(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if deleted == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "api token not found"})
		return
	}

	// The token is deleted, so the entry points at its owner to keep that known.
	utils.RecordAuditLog(c, "revoked-api-token", "user", token.UserId)
	sse.PublishUpdate(sse.TopicAPITokensOf(token.UserId), sse.TopicAdminAPITokens)

	c.JSON(http.StatusOK, gin.H{"message": "api token revoked"})
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
		ExpiresAt:  token.GrantExpiresAt().Format(time.RFC3339),
		LastUsedAt: lastUsedAt,
		CreatedAt:  token.CreatedAt.Format(time.RFC3339),
		OAuth:      token.IsOAuth(),
	}
}
