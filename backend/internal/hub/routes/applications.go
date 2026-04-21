package routes

import (
	"errors"
	"net/http"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/repositories"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const ApplicationsPath = "/api/v1/applications"

type createApplicationRequest struct {
	Name         string `json:"name" binding:"required"`
	RepositoryId string `json:"repositoryId" binding:"required"`
	AgentId      string `json:"agentId" binding:"required"`
	Branch       string `json:"branch" binding:"required"`
	Path         string `json:"path" binding:"required"`
}

type updateApplicationRequest struct {
	Name         string `json:"name" binding:"required"`
	RepositoryId string `json:"repositoryId" binding:"required"`
	AgentId      string `json:"agentId" binding:"required"`
	Branch       string `json:"branch" binding:"required"`
	Path         string `json:"path" binding:"required"`
}

type applicationListResponse struct {
	Id             string  `json:"id"`
	Name           string  `json:"name"`
	HealthStatus   string  `json:"healthStatus"`
	SyncStatus     string  `json:"syncStatus"`
	Branch         string  `json:"branch"`
	Commit         string  `json:"commit"`
	LastSyncedAt   *string `json:"lastSyncedAt"`
	Path           string  `json:"path"`
	AgentName      string  `json:"agentName"`
	RepositoryName string  `json:"repositoryName"`
}

type applicationResponse struct {
	Id             string  `json:"id"`
	Name           string  `json:"name"`
	RepositoryId   string  `json:"repositoryId"`
	RepositoryName string  `json:"repositoryName"`
	AgentId        string  `json:"agentId"`
	AgentName      string  `json:"agentName"`
	SyncStatus     string  `json:"syncStatus"`
	HealthStatus   string  `json:"healthStatus"`
	Branch         string  `json:"branch"`
	Commit         string  `json:"commit"`
	CommitMessage  string  `json:"commitMessage"`
	LastSyncedAt   *string `json:"lastSyncedAt"`
	Path           string  `json:"path"`
	CreatedAt      string  `json:"createdAt"`
	UpdatedAt      string  `json:"updatedAt"`
	ComposeFile    string  `json:"composeFile"`
}

func ListApplicationsHandler(c *gin.Context) {
	applications, err := gorm.G[models.Application](db.DB).
		Preload("Repository", nil).
		Preload("Agent", nil).
		Order("created_at ASC").
		Find(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	response := make([]applicationListResponse, 0, len(applications))
	for i := range applications {
		response = append(response, toApplicationListResponse(&applications[i]))
	}

	c.JSON(http.StatusOK, response)
}

func GetApplicationHandler(c *gin.Context) {
	id := c.Param("id")

	application, err := gorm.G[models.Application](db.DB).
		Preload("Repository", nil).
		Preload("Agent", nil).
		Where("id = ?", id).
		First(c.Request.Context())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "application not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, toApplicationResponse(&application))
}

func CreateApplicationHandler(c *gin.Context) {
	var req createApplicationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: name, repositoryId, agentId, syncStatus, healthStatus, branch, commit, commitMessage, and path are required"})
		return
	}

	repoExists, err := hasRecord[models.Repository](c, req.RepositoryId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if !repoExists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repository not found"})
		return
	}

	agentExists, err := hasRecord[models.Agent](c, req.AgentId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if !agentExists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent not found"})
		return
	}

	composeFile, latestCommit, statusCode, sourceErr := fetchApplicationComposeAndCommit(c, req.RepositoryId, req.Branch, req.Path)
	if sourceErr != "" {
		c.JSON(statusCode, gin.H{"error": sourceErr})
		return
	}

	application := models.Application{
		Name:          crypto.EncryptedString(req.Name),
		RepositoryId:  req.RepositoryId,
		AgentId:       req.AgentId,
		SyncStatus:    models.UnknownSync,
		HealthStatus:  models.UnknownHealth,
		Branch:        req.Branch,
		Commit:        latestCommit.Hash,
		CommitMessage: latestCommit.Message,
		LastSyncedAt:  nil,
		Path:          req.Path,
		ComposeFile:   crypto.EncryptedString(composeFile),
	}

	if err := gorm.G[models.Application](db.DB).Select("*").Create(c.Request.Context(), &application); err != nil {
		if errors.Is(err, gorm.ErrForeignKeyViolated) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid repositoryId or agentId"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	createdApplication, err := gorm.G[models.Application](db.DB).
		Preload("Repository", nil).
		Preload("Agent", nil).
		Where("id = ?", application.Id).
		First(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusCreated, toApplicationResponse(&createdApplication))
	sse.PublishUpdate(ApplicationsPath)
}

func UpdateApplicationHandler(c *gin.Context) {
	id := c.Param("id")

	var req updateApplicationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: name, repositoryId, agentId, syncStatus, healthStatus, branch, commit, commitMessage, and path are required"})
		return
	}

	application, err := gorm.G[models.Application](db.DB).Where("id = ?", id).First(c.Request.Context())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "application not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	repoExists, err := hasRecord[models.Repository](c, req.RepositoryId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if !repoExists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repository not found"})
		return
	}

	agentExists, err := hasRecord[models.Agent](c, req.AgentId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if !agentExists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent not found"})
		return
	}

	composeFile, latestCommit, statusCode, sourceErr := fetchApplicationComposeAndCommit(c, req.RepositoryId, req.Branch, req.Path)
	if sourceErr != "" {
		c.JSON(statusCode, gin.H{"error": sourceErr})
		return
	}

	application.Name = crypto.EncryptedString(req.Name)
	application.RepositoryId = req.RepositoryId
	application.AgentId = req.AgentId
	application.Branch = req.Branch
	application.Commit = latestCommit.Hash
	application.CommitMessage = latestCommit.Message
	application.Path = req.Path
	application.ComposeFile = crypto.EncryptedString(composeFile)

	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", id).Select("*").Updates(c.Request.Context(), application); err != nil {
		if errors.Is(err, gorm.ErrForeignKeyViolated) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid repositoryId or agentId"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	updatedApplication, err := gorm.G[models.Application](db.DB).
		Preload("Repository", nil).
		Preload("Agent", nil).
		Where("id = ?", id).
		First(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, toApplicationResponse(&updatedApplication))
	sse.PublishUpdate(ApplicationsPath)
}

func DeleteApplicationHandler(c *gin.Context) {
	id := c.Param("id")

	rowsAffected, err := gorm.G[models.Application](db.DB).Where("id = ?", id).Delete(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "application not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "application deleted"})
	sse.PublishUpdate(ApplicationsPath)
}

func toApplicationListResponse(app *models.Application) applicationListResponse {
	return applicationListResponse{
		Id:             app.Id,
		Name:           app.Name.String(),
		HealthStatus:   string(app.HealthStatus),
		SyncStatus:     string(app.SyncStatus),
		Branch:         app.Branch,
		Commit:         app.Commit,
		LastSyncedAt:   formatTimestamp(app.LastSyncedAt),
		Path:           app.Path,
		RepositoryName: app.Repository.Name,
		AgentName:      app.Agent.Name.String(),
	}
}

func toApplicationResponse(app *models.Application) applicationResponse {
	return applicationResponse{
		Id:             app.Id,
		Name:           app.Name.String(),
		RepositoryId:   app.RepositoryId,
		RepositoryName: app.Repository.Name,
		AgentId:        app.AgentId,
		AgentName:      app.Agent.Name.String(),
		SyncStatus:     string(app.SyncStatus),
		HealthStatus:   string(app.HealthStatus),
		Branch:         app.Branch,
		Commit:         app.Commit,
		CommitMessage:  app.CommitMessage,
		LastSyncedAt:   formatTimestamp(app.LastSyncedAt),
		Path:           app.Path,
		CreatedAt:      app.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      app.UpdatedAt.Format(time.RFC3339),
		ComposeFile:    app.ComposeFile.String(),
	}
}

func fetchApplicationComposeAndCommit(c *gin.Context, repositoryID, branch, path string) (string, repositories.CommitInfo, int, string) {
	repo, err := gorm.G[models.Repository](db.DB).Where("id = ?", repositoryID).First(c.Request.Context())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", repositories.CommitInfo{}, http.StatusBadRequest, "repository not found"
		}
		return "", repositories.CommitInfo{}, http.StatusInternalServerError, "internal server error"
	}

	provider, err := repositories.Get(repo.Provider)
	if err != nil {
		return "", repositories.CommitInfo{}, http.StatusBadRequest, "unsupported provider"
	}

	latestCommit, err := provider.GetLatestCommit(c.Request.Context(), &repo, branch)
	if err != nil {
		return "", repositories.CommitInfo{}, http.StatusUnprocessableEntity, err.Error()
	}

	composeFile, err := provider.GetFileContent(c.Request.Context(), &repo, latestCommit.Hash, path)
	if err != nil {
		return "", repositories.CommitInfo{}, http.StatusUnprocessableEntity, err.Error()
	}

	return composeFile, latestCommit, 0, ""
}

func parseRFC3339Timestamp(value *string) (*time.Time, bool) {
	if value == nil || *value == "" {
		return nil, true
	}

	parsed, err := time.Parse(time.RFC3339, *value)
	if err != nil {
		return nil, false
	}

	return &parsed, true
}

func formatTimestamp(value *time.Time) *string {
	if value == nil {
		return nil
	}

	formatted := value.Format(time.RFC3339)
	return &formatted
}

func hasRecord[T any](c *gin.Context, id string) (bool, error) {
	count, err := gorm.G[T](db.DB).Where("id = ?", id).Count(c.Request.Context(), "*")
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
