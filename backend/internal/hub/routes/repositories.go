package routes

import (
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/repositories"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type createRepositoryRequest struct {
	Name            string                      `json:"name" binding:"required,min=1,max=50"`
	Url             string                      `json:"url" binding:"required"`
	Provider        models.RepositoryProvider   `json:"provider" binding:"required"`
	AuthMethod      models.RepositoryAuthMethod `json:"authMethod" binding:"required"`
	AuthUser        *string                     `json:"authUser"`
	AuthToken       *string                     `json:"authToken"`
	SyncType        models.RepositorySyncType   `json:"syncType" binding:"required"`
	PollingInterval *int64                      `json:"pollingIntervalSeconds"`
	WebhookSecret   *string                     `json:"webhookSecret"`
}

type repositoryResponse struct {
	Id                     string  `json:"id"`
	Name                   string  `json:"name"`
	Url                    string  `json:"url"`
	Provider               string  `json:"provider"`
	AuthMethod             string  `json:"authMethod"`
	SyncType               string  `json:"syncType"`
	SyncStatus             string  `json:"syncStatus"`
	LastSyncError          *string `json:"lastSyncError"`
	PollingIntervalSeconds *int64  `json:"pollingIntervalSeconds"`
	LastSyncedAt           *string `json:"lastSyncedAt"`
	CreatedBy              string  `json:"createdBy"`
	CreatedAt              string  `json:"createdAt"`
	UpdatedAt              string  `json:"updatedAt"`
}

func toRepositoryResponse(r *models.Repository) repositoryResponse {
	resp := repositoryResponse{
		Id:            r.Id,
		Name:          r.Name,
		Url:           r.Url,
		Provider:      string(r.Provider),
		AuthMethod:    string(r.AuthMethod),
		SyncType:      string(r.SyncType),
		SyncStatus:    string(r.SyncStatus),
		LastSyncError: r.LastSyncError,
		CreatedBy:     r.CreatedBy,
		CreatedAt:     r.CreatedAt.Format(time.RFC3339),
		UpdatedAt:     r.UpdatedAt.Format(time.RFC3339),
	}

	if r.PollingInterval != nil {
		secs := int64(*r.PollingInterval / time.Second)
		resp.PollingIntervalSeconds = &secs
	}

	if r.LastSyncedAt != nil {
		s := r.LastSyncedAt.Format(time.RFC3339)
		resp.LastSyncedAt = &s
	}

	return resp
}

func ListRepositoriesHandler(c *gin.Context) {
	repos, err := gorm.G[models.Repository](db.DB).Find(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	result := make([]repositoryResponse, 0, len(repos))
	for _, r := range repos {
		result = append(result, toRepositoryResponse(&r))
	}

	c.JSON(http.StatusOK, result)
}

func CreateRepositoryHandler(c *gin.Context) {
	claims, ok := auth.GetClaims(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authentication"})
		return
	}

	var req createRepositoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: name, url, provider, authMethod, and syncType are required"})
		return
	}

	// Validate syncType value
	switch req.SyncType {
	case models.SyncTypePolling, models.SyncTypeWebhook, models.SyncTypeManual:
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid syncType: must be polling, webhook, or manual"})
		return
	}

	// Validate polling interval is provided when syncType is polling
	if req.SyncType == models.SyncTypePolling && req.PollingInterval == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pollingIntervalSeconds is required when syncType is polling"})
		return
	}

	if _, httpStatus, validationErr := resolveProvider(req.Provider, req.Url, req.AuthMethod); validationErr != "" {
		c.JSON(httpStatus, gin.H{"error": validationErr})
		return
	}

	repo := models.Repository{
		Name:       req.Name,
		Url:        req.Url,
		Provider:   req.Provider,
		AuthMethod: req.AuthMethod,
		SyncType:   req.SyncType,
		SyncStatus: models.SyncStatusUnknown,
		CreatedBy:  claims.Subject,
	}

	if req.AuthUser != nil && *req.AuthUser != "" {
		enc := crypto.EncryptedString(*req.AuthUser)
		repo.AuthUser = &enc
	}

	if req.AuthToken != nil && *req.AuthToken != "" {
		enc := crypto.EncryptedString(*req.AuthToken)
		repo.AuthToken = &enc
	}

	if req.WebhookSecret != nil && *req.WebhookSecret != "" {
		enc := crypto.EncryptedString(*req.WebhookSecret)
		repo.WebhookSecret = &enc
	}

	if req.PollingInterval != nil {
		d := time.Duration(*req.PollingInterval) * time.Second
		repo.PollingInterval = &d
	}

	if err := db.DB.WithContext(c.Request.Context()).Select("*").Create(&repo).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			c.JSON(http.StatusConflict, gin.H{"error": "repository already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusCreated, toRepositoryResponse(&repo))
}

type testConnectionRequest struct {
	Url        string                      `json:"url" binding:"required"`
	Provider   models.RepositoryProvider   `json:"provider" binding:"required"`
	AuthMethod models.RepositoryAuthMethod `json:"authMethod" binding:"required"`
	AuthUser   *string                     `json:"authUser"`
	AuthToken  *string                     `json:"authToken"`
}

func TestConnectionHandler(c *gin.Context) {
	var req testConnectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: url, provider, and authMethod are required"})
		return
	}

	provider, httpStatus, validationErr := resolveProvider(req.Provider, req.Url, req.AuthMethod)
	if validationErr != "" {
		c.JSON(httpStatus, gin.H{"error": validationErr})
		return
	}

	repo := &models.Repository{
		Url:        req.Url,
		Provider:   req.Provider,
		AuthMethod: req.AuthMethod,
	}

	if req.AuthUser != nil && *req.AuthUser != "" {
		enc := crypto.EncryptedString(*req.AuthUser)
		repo.AuthUser = &enc
	}

	if req.AuthToken != nil && *req.AuthToken != "" {
		enc := crypto.EncryptedString(*req.AuthToken)
		repo.AuthToken = &enc
	}

	if err := provider.TestConnection(c.Request.Context(), repo); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "connection successful"})
}

// resolveProvider validates the provider enum, URL, and authMethod, returning the
// registered provider on success, or an HTTP status code and error message on failure.
func resolveProvider(
	prov models.RepositoryProvider,
	url string,
	authMethod models.RepositoryAuthMethod,
) (repositories.Provider, int, string) {
	switch prov {
	case models.GitHub, models.GitLab, models.Generic, models.Bitbucket, models.AzureDevOps, models.Gitea:
	default:
		return nil, http.StatusBadRequest, "invalid provider: must be github, gitlab, or generic"
	}

	switch authMethod {
	case models.AuthMethodNone, models.AuthMethodToken, models.AuthMethodBasic, models.AuthMethodSSH:
	default:
		return nil, http.StatusBadRequest, "invalid authMethod: must be none, token, basic, or ssh"
	}

	p, err := repositories.Get(prov)
	if err != nil {
		return nil, http.StatusBadRequest, "unsupported provider"
	}

	if err := p.ValidateURL(url); err != nil {
		return nil, http.StatusBadRequest, "invalid repository URL: " + err.Error()
	}

	if !isAuthMethodSupported(authMethod, p.SupportedAuthMethods()) {
		return nil, http.StatusBadRequest, "authMethod is not supported by this provider"
	}

	return p, 0, ""
}

func isAuthMethodSupported(method models.RepositoryAuthMethod, supported []models.RepositoryAuthMethod) bool {
	return slices.Contains(supported, method)
}
