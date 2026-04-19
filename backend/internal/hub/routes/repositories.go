package routes

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/repositories"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const RepositoriesPath = "/api/v1/repositories"

var appUrl string

func SetRepositoriesConfig(url string) {
	appUrl = url
}

type createRepositoryRequest struct {
	Url             string                      `json:"url" binding:"required"`
	Provider        models.RepositoryProvider   `json:"provider" binding:"required"`
	AuthMethod      models.RepositoryAuthMethod `json:"authMethod" binding:"required"`
	AuthUser        *string                     `json:"authUser"`
	AuthToken       *string                     `json:"authToken"`
	SyncType        models.RepositorySyncType   `json:"syncType" binding:"required"`
	PollingInterval *int64                      `json:"pollingIntervalSeconds"`
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
	WebhookSecret          *string `json:"webhookSecret,omitempty"`
	WebhookUrl             *string `json:"webhookUrl,omitempty"`
	AppCount               int     `json:"appCount"`
}

func toRepositoryResponse(r *models.Repository, includeWebhook bool) repositoryResponse {
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
		AppCount:      0, // This will be populated in the future
	}

	if r.PollingInterval != nil {
		secs := int64(*r.PollingInterval / time.Second)
		resp.PollingIntervalSeconds = &secs
	}

	if r.LastSyncedAt != nil {
		s := r.LastSyncedAt.Format(time.RFC3339)
		resp.LastSyncedAt = &s
	}

	if includeWebhook && r.WebhookSecret != nil {
		s := r.WebhookSecret.String()
		resp.WebhookSecret = &s

		// Webhook URL would typically be something like: https://orca-cd.example.com/webhooks/repositories/{id}
		webhookUrl := fmt.Sprintf("%s/api/v1/webhooks/%s", appUrl, r.Id)
		resp.WebhookUrl = &webhookUrl
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
		result = append(result, toRepositoryResponse(&r, false))
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: url, provider, authMethod, and syncType are required"})
		return
	}

	// Validate syncType value
	switch req.SyncType {
	case models.SyncTypePolling, models.SyncTypeWebhook, models.SyncTypeManual:
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid syncType: must be polling, webhook, or manual"})
		return
	}

	if req.SyncType == models.SyncTypePolling && req.PollingInterval == nil {
		// Default to 60 seconds if polling is selected but no interval is provided
		var defaultInterval int64 = 60
		req.PollingInterval = &defaultInterval
	}

	provider, httpStatus, validationErr := resolveProvider(req.Provider, req.AuthMethod)
	if validationErr != "" {
		c.JSON(httpStatus, gin.H{"error": validationErr})
		return
	}

	repoOwner, repoName, err := provider.ParseURL(req.Url)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid repository URL: %v", err)})
		return
	}

	repo := models.Repository{
		Name:       fmt.Sprintf("%s/%s", repoOwner, repoName),
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

	if req.SyncType == models.SyncTypeWebhook {
		secret, err := auth.GenerateRandomString(32)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate webhook secret"})
			return
		}
		enc := crypto.EncryptedString(secret)
		repo.WebhookSecret = &enc
	}

	if req.PollingInterval != nil {
		d := time.Duration(*req.PollingInterval) * time.Second
		repo.PollingInterval = &d
	}

	if err := db.DB.WithContext(c.Request.Context()).Select("*").Create(&repo).Error; err != nil {
		if isUniqueConstraintError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "repository already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusCreated, toRepositoryResponse(&repo, true))
	sse.PublishUpdate(RepositoriesPath)
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

	provider, httpStatus, validationErr := resolveProvider(req.Provider, req.AuthMethod)
	if validationErr != "" {
		c.JSON(httpStatus, gin.H{"error": validationErr})
		return
	}

	_, _, err := provider.ParseURL(req.Url)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid repository URL: %v", err)})
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

func DeleteRepositoryHandler(c *gin.Context) {
	id := c.Param("id")

	result := db.DB.WithContext(c.Request.Context()).Where("id = ?", id).Delete(&models.Repository{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "repository not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "repository deleted"})
	sse.PublishUpdate(RepositoriesPath)
}

type updateRepositoryRequest struct {
	Url             string                      `json:"url" binding:"required"`
	AuthMethod      models.RepositoryAuthMethod `json:"authMethod" binding:"required"`
	AuthUser        *string                     `json:"authUser"`
	AuthToken       *string                     `json:"authToken"`
	SyncType        models.RepositorySyncType   `json:"syncType" binding:"required"`
	PollingInterval *int64                      `json:"pollingIntervalSeconds"`
}

func UpdateRepositoryHandler(c *gin.Context) {
	id := c.Param("id")

	var req updateRepositoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: url, authMethod, and syncType are required"})
		return
	}

	switch req.SyncType {
	case models.SyncTypePolling, models.SyncTypeWebhook, models.SyncTypeManual:
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid syncType: must be polling, webhook, or manual"})
		return
	}

	if req.SyncType == models.SyncTypePolling && req.PollingInterval == nil {
		var defaultInterval int64 = 60
		req.PollingInterval = &defaultInterval
	}

	repo, err := gorm.G[models.Repository](db.DB).Where("id = ?", id).First(c.Request.Context())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "repository not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	provider, httpStatus, validationErr := resolveProvider(repo.Provider, req.AuthMethod)
	if validationErr != "" {
		c.JSON(httpStatus, gin.H{"error": validationErr})
		return
	}

	if req.Url != repo.Url {
		repoOwner, repoName, err := provider.ParseURL(req.Url)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid repository URL: %v", err)})
			return
		}
		repo.Url = req.Url
		repo.Name = fmt.Sprintf("%s/%s", repoOwner, repoName)
	}

	repo.AuthMethod = req.AuthMethod

	if req.AuthUser != nil && *req.AuthUser != "" {
		enc := crypto.EncryptedString(*req.AuthUser)
		repo.AuthUser = &enc
	} else {
		repo.AuthUser = nil
	}

	if req.AuthToken != nil && *req.AuthToken != "" {
		enc := crypto.EncryptedString(*req.AuthToken)
		repo.AuthToken = &enc
	} else {
		repo.AuthToken = nil
	}

	prevSyncType := repo.SyncType
	repo.SyncType = req.SyncType

	switch {
	case req.SyncType == models.SyncTypeWebhook && prevSyncType != models.SyncTypeWebhook:
		secret, err := auth.GenerateRandomString(32)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate webhook secret"})
			return
		}
		enc := crypto.EncryptedString(secret)
		repo.WebhookSecret = &enc
	case req.SyncType != models.SyncTypeWebhook:
		repo.WebhookSecret = nil
	}

	if req.PollingInterval != nil {
		d := time.Duration(*req.PollingInterval) * time.Second
		repo.PollingInterval = &d
	} else {
		repo.PollingInterval = nil
	}

	if err := db.DB.WithContext(c.Request.Context()).Save(&repo).Error; err != nil {
		if isUniqueConstraintError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "repository already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	newWebhookSecret := req.SyncType == models.SyncTypeWebhook && prevSyncType != models.SyncTypeWebhook
	c.JSON(http.StatusOK, toRepositoryResponse(&repo, newWebhookSecret))
	sse.PublishUpdate(RepositoriesPath)
}

// resolveProvider validates the provider enum, URL, and authMethod, returning the
// registered provider on success, or an HTTP status code and error message on failure.
func resolveProvider(
	prov models.RepositoryProvider,
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

	if !isAuthMethodSupported(authMethod, p.SupportedAuthMethods()) {
		return nil, http.StatusBadRequest, "authMethod is not supported by this provider"
	}

	return p, 0, ""
}

func isAuthMethodSupported(method models.RepositoryAuthMethod, supported []models.RepositoryAuthMethod) bool {
	return slices.Contains(supported, method)
}
