package routes

import (
	"errors"
	"net/http"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type createApplicationRequest struct {
	Name          string              `json:"name" binding:"required"`
	RepositoryId  string              `json:"repositoryId" binding:"required"`
	AgentId       string              `json:"agentId" binding:"required"`
	SyncStatus    models.SyncStatus   `json:"syncStatus" binding:"required"`
	HealthStatus  models.HealthStatus `json:"healthStatus" binding:"required"`
	Branch        string              `json:"branch" binding:"required"`
	Commit        string              `json:"commit" binding:"required"`
	CommitMessage string              `json:"commitMessage" binding:"required"`
	LastSyncedAt  *string             `json:"lastSyncedAt"`
	Path          string              `json:"path" binding:"required"`
}

type updateApplicationRequest struct {
	Name          string              `json:"name" binding:"required"`
	RepositoryId  string              `json:"repositoryId" binding:"required"`
	AgentId       string              `json:"agentId" binding:"required"`
	SyncStatus    models.SyncStatus   `json:"syncStatus" binding:"required"`
	HealthStatus  models.HealthStatus `json:"healthStatus" binding:"required"`
	Branch        string              `json:"branch" binding:"required"`
	Commit        string              `json:"commit" binding:"required"`
	CommitMessage string              `json:"commitMessage" binding:"required"`
	LastSyncedAt  *string             `json:"lastSyncedAt"`
	Path          string              `json:"path" binding:"required"`
}

type applicationListResponse struct {
	Id           string  `json:"id"`
	HealthStatus string  `json:"healthStatus"`
	SyncStatus   string  `json:"syncStatus"`
	Branch       string  `json:"branch"`
	Commit       string  `json:"commit"`
	LastSyncedAt *string `json:"lastSyncedAt"`
}

type applicationResponse struct {
	Id            string  `json:"id"`
	Name          string  `json:"name"`
	RepositoryId  string  `json:"repositoryId"`
	AgentId       string  `json:"agentId"`
	SyncStatus    string  `json:"syncStatus"`
	HealthStatus  string  `json:"healthStatus"`
	Branch        string  `json:"branch"`
	Commit        string  `json:"commit"`
	CommitMessage string  `json:"commitMessage"`
	LastSyncedAt  *string `json:"lastSyncedAt"`
	Path          string  `json:"path"`
	CreatedAt     string  `json:"createdAt"`
	UpdatedAt     string  `json:"updatedAt"`
}

func ListApplicationsHandler(c *gin.Context) {
	var applications []models.Application
	if err := db.DB.WithContext(c.Request.Context()).Order("created_at ASC").Find(&applications).Error; err != nil {
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

	application, err := gorm.G[models.Application](db.DB).Where("id = ?", id).First(c.Request.Context())
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

	if !isValidSyncStatus(req.SyncStatus) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid syncStatus: must be synced, out_of_sync, progressing, or unknown"})
		return
	}

	if !isValidHealthStatus(req.HealthStatus) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid healthStatus: must be healthy, unhealthy, or unknown"})
		return
	}

	lastSyncedAt, ok := parseRFC3339Timestamp(req.LastSyncedAt)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid lastSyncedAt: must be RFC3339"})
		return
	}

	repoExists, err := hasRecord(c, &models.Repository{}, req.RepositoryId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if !repoExists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repository not found"})
		return
	}

	agentExists, err := hasRecord(c, &models.Agent{}, req.AgentId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if !agentExists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent not found"})
		return
	}

	application := models.Application{
		Name:          crypto.EncryptedString(req.Name),
		RepositoryId:  req.RepositoryId,
		AgentId:       req.AgentId,
		SyncStatus:    req.SyncStatus,
		HealthStatus:  req.HealthStatus,
		Branch:        req.Branch,
		Commit:        req.Commit,
		CommitMessage: req.CommitMessage,
		LastSyncedAt:  lastSyncedAt,
		Path:          req.Path,
	}

	if err := db.DB.WithContext(c.Request.Context()).Select("*").Create(&application).Error; err != nil {
		if errors.Is(err, gorm.ErrForeignKeyViolated) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid repositoryId or agentId"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusCreated, toApplicationResponse(&application))
}

func UpdateApplicationHandler(c *gin.Context) {
	id := c.Param("id")

	var req updateApplicationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: name, repositoryId, agentId, syncStatus, healthStatus, branch, commit, commitMessage, and path are required"})
		return
	}

	if !isValidSyncStatus(req.SyncStatus) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid syncStatus: must be synced, out_of_sync, progressing, or unknown"})
		return
	}

	if !isValidHealthStatus(req.HealthStatus) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid healthStatus: must be healthy, unhealthy, or unknown"})
		return
	}

	lastSyncedAt, ok := parseRFC3339Timestamp(req.LastSyncedAt)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid lastSyncedAt: must be RFC3339"})
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

	repoExists, err := hasRecord(c, &models.Repository{}, req.RepositoryId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if !repoExists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repository not found"})
		return
	}

	agentExists, err := hasRecord(c, &models.Agent{}, req.AgentId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if !agentExists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent not found"})
		return
	}

	application.Name = crypto.EncryptedString(req.Name)
	application.RepositoryId = req.RepositoryId
	application.AgentId = req.AgentId
	application.SyncStatus = req.SyncStatus
	application.HealthStatus = req.HealthStatus
	application.Branch = req.Branch
	application.Commit = req.Commit
	application.CommitMessage = req.CommitMessage
	application.LastSyncedAt = lastSyncedAt
	application.Path = req.Path

	if err := db.DB.WithContext(c.Request.Context()).Save(&application).Error; err != nil {
		if errors.Is(err, gorm.ErrForeignKeyViolated) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid repositoryId or agentId"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, toApplicationResponse(&application))
}

func DeleteApplicationHandler(c *gin.Context) {
	id := c.Param("id")

	result := db.DB.WithContext(c.Request.Context()).Where("id = ?", id).Delete(&models.Application{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "application not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "application deleted"})
}

func toApplicationListResponse(app *models.Application) applicationListResponse {
	return applicationListResponse{
		Id:           app.Id,
		HealthStatus: string(app.HealthStatus),
		SyncStatus:   string(app.SyncStatus),
		Branch:       app.Branch,
		Commit:       app.Commit,
		LastSyncedAt: formatTimestamp(app.LastSyncedAt),
	}
}

func toApplicationResponse(app *models.Application) applicationResponse {
	return applicationResponse{
		Id:            app.Id,
		Name:          app.Name.String(),
		RepositoryId:  app.RepositoryId,
		AgentId:       app.AgentId,
		SyncStatus:    string(app.SyncStatus),
		HealthStatus:  string(app.HealthStatus),
		Branch:        app.Branch,
		Commit:        app.Commit,
		CommitMessage: app.CommitMessage,
		LastSyncedAt:  formatTimestamp(app.LastSyncedAt),
		Path:          app.Path,
		CreatedAt:     app.CreatedAt.Format(time.RFC3339),
		UpdatedAt:     app.UpdatedAt.Format(time.RFC3339),
	}
}

func isValidSyncStatus(status models.SyncStatus) bool {
	switch status {
	case models.Synced, models.OutOfSync, models.Progressing, models.UnknownSync:
		return true
	default:
		return false
	}
}

func isValidHealthStatus(status models.HealthStatus) bool {
	switch status {
	case models.Healthy, models.Unhealthy, models.UnknownHealth:
		return true
	default:
		return false
	}
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

func hasRecord(c *gin.Context, model any, id string) (bool, error) {
	var count int64
	err := db.DB.WithContext(c.Request.Context()).Model(model).Where("id = ?", id).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
