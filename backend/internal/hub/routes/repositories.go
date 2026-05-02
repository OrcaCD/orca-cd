package routes

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/applications"
	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/repositories"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	RepositoriesPath                    = "/api/v1/repositories"
	DefaultPollingIntervalSeconds int64 = 60
	MinPollingIntervalSeconds     int64 = 30
)

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

func toRepositoryResponse(r *models.Repository, includeWebhook bool, appCount int) repositoryResponse {
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
		AppCount:      appCount,
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

type appCountByRepositoryRow struct {
	RepositoryId string `gorm:"column:repository_id"`
	AppCount     int64  `gorm:"column:app_count"`
}

func countApplicationsByRepositoryID(ctx context.Context, repositoryId string) (int, error) {
	count, err := gorm.G[models.Application](db.DB).Where("repository_id = ?", repositoryId).Count(ctx, "*")
	if err != nil {
		return 0, err
	}

	return int(count), nil
}

func countApplicationsByRepositoryIDs(ctx context.Context, repositoryIds []string) (map[string]int, error) {
	counts := make(map[string]int, len(repositoryIds))
	if len(repositoryIds) == 0 {
		return counts, nil
	}

	rows := []appCountByRepositoryRow{}
	if err := db.DB.WithContext(ctx).
		Model(&models.Application{}).
		Select("repository_id, COUNT(*) AS app_count").
		Where("repository_id IN ?", repositoryIds).
		Group("repository_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	for i := range rows {
		counts[rows[i].RepositoryId] = int(rows[i].AppCount)
	}

	return counts, nil
}

func ListRepositoriesHandler(c *gin.Context) {
	ctx := c.Request.Context()

	repos, err := gorm.G[models.Repository](db.DB).Find(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	repositoryIds := make([]string, 0, len(repos))
	for i := range repos {
		repositoryIds = append(repositoryIds, repos[i].Id)
	}

	appCountsByRepositoryId, err := countApplicationsByRepositoryIDs(ctx, repositoryIds)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	result := make([]repositoryResponse, 0, len(repos))
	for i := range repos {
		result = append(result, toRepositoryResponse(&repos[i], false, appCountsByRepositoryId[repos[i].Id]))
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
		var defaultInterval = DefaultPollingIntervalSeconds
		req.PollingInterval = &defaultInterval
	}

	if req.SyncType == models.SyncTypePolling && req.PollingInterval != nil && *req.PollingInterval < MinPollingIntervalSeconds {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("pollingIntervalSeconds must be at least %d", MinPollingIntervalSeconds)})
		return
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

	appCount, err := countApplicationsByRepositoryID(c.Request.Context(), repo.Id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusCreated, toRepositoryResponse(&repo, true, appCount))
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

func ListRepositoryBranchesHandler(c *gin.Context) {
	repoID := c.Param("id")
	repo, provider, ok := resolveRepositoryByID(c, repoID)
	if !ok {
		return
	}

	branches, err := provider.ListBranches(c.Request.Context(), &repo)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, branches)
}

func ListRepositoryTreeHandler(c *gin.Context) {
	repoID := c.Param("id")
	branch := strings.TrimSpace(c.Query("branch"))
	if branch == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "branch query parameter is required"})
		return
	}

	repo, provider, ok := resolveRepositoryByID(c, repoID)
	if !ok {
		return
	}

	entries, err := provider.ListTree(c.Request.Context(), &repo, branch)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, entries)
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
	AuthMethod      *models.RepositoryAuthMethod `json:"authMethod"`
	AuthUser        *string                      `json:"authUser"`
	AuthToken       *string                      `json:"authToken"`
	SyncType        *models.RepositorySyncType   `json:"syncType"`
	PollingInterval *int64                       `json:"pollingIntervalSeconds"`
}

func UpdateRepositoryHandler(c *gin.Context) {
	id := c.Param("id")

	var req updateRepositoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	if req.SyncType != nil {
		switch *req.SyncType {
		case models.SyncTypePolling, models.SyncTypeWebhook, models.SyncTypeManual:
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid syncType: must be polling, webhook, or manual"})
			return
		}

		if *req.SyncType == models.SyncTypePolling && req.PollingInterval == nil {
			var defaultInterval = DefaultPollingIntervalSeconds
			req.PollingInterval = &defaultInterval
		}

		if *req.SyncType == models.SyncTypePolling && req.PollingInterval != nil && *req.PollingInterval < MinPollingIntervalSeconds {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("pollingIntervalSeconds must be at least %d", MinPollingIntervalSeconds)})
			return
		}
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

	if req.AuthMethod != nil {
		authMethod := *req.AuthMethod
		_, httpStatus, validationErr := resolveProvider(repo.Provider, authMethod)
		if validationErr != "" {
			c.JSON(httpStatus, gin.H{"error": validationErr})
			return
		}

		repo.AuthMethod = authMethod

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
	}

	prevSyncType := repo.SyncType
	if req.SyncType != nil {
		repo.SyncType = *req.SyncType

		switch {
		case *req.SyncType == models.SyncTypeWebhook && prevSyncType != models.SyncTypeWebhook:
			secret, err := auth.GenerateRandomString(32)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate webhook secret"})
				return
			}
			enc := crypto.EncryptedString(secret)
			repo.WebhookSecret = &enc
		case *req.SyncType != models.SyncTypeWebhook:
			repo.WebhookSecret = nil
		}

		if req.PollingInterval != nil {
			d := time.Duration(*req.PollingInterval) * time.Second
			repo.PollingInterval = &d
		} else {
			repo.PollingInterval = nil
		}
	}

	if err := db.DB.WithContext(c.Request.Context()).Save(&repo).Error; err != nil {
		if isUniqueConstraintError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "repository already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	appCount, err := countApplicationsByRepositoryID(c.Request.Context(), repo.Id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	newWebhookSecret := req.SyncType != nil && *req.SyncType == models.SyncTypeWebhook && prevSyncType != models.SyncTypeWebhook
	c.JSON(http.StatusOK, toRepositoryResponse(&repo, newWebhookSecret, appCount))
	sse.PublishUpdate(RepositoriesPath)
}

func SyncRepositoryHandler(c *gin.Context) {
	id := c.Param("id")
	repo, err := gorm.G[models.Repository](db.DB).Where("id = ?", id).First(c.Request.Context())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "repository not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	if _, err := repositories.Get(repo.Provider); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported provider"})
		return
	}

	if applications.DefaultPoller == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "application poller not initialized"})
		return
	}

	applications.DefaultPoller.TriggerSync(&repo)

	c.JSON(http.StatusAccepted, gin.H{"message": "sync triggered"})
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

func resolveRepositoryByID(c *gin.Context, id string) (models.Repository, repositories.Provider, bool) {
	repo, err := gorm.G[models.Repository](db.DB).Where("id = ?", id).First(c.Request.Context())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "repository not found"})
			return models.Repository{}, nil, false
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return models.Repository{}, nil, false
	}

	provider, err := repositories.Get(repo.Provider)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported provider"})
		return models.Repository{}, nil, false
	}

	return repo, provider, true
}
