package routes

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	application_deployer "github.com/OrcaCD/orca-cd/internal/hub/deployer"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/repositories"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/OrcaCD/orca-cd/internal/hub/websocket"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const ApplicationsPath = "/api/v1/applications"
const defaultApplicationIcon = "box"
const agentDeleteTimeout = 30 * time.Second

type createApplicationRequest struct {
	Name                     string `json:"name" binding:"required"`
	Icon                     string `json:"icon" binding:"omitempty,min=1,max=128"`
	RepositoryId             string `json:"repositoryId" binding:"required"`
	AgentId                  string `json:"agentId" binding:"required"`
	Branch                   string `json:"branch" binding:"required"`
	Path                     string `json:"path" binding:"required"`
	ImagePollEnabled         bool   `json:"imagePollEnabled"`
	ImagePollIntervalSeconds int64  `json:"imagePollIntervalSeconds"`
	ImagePollDeleteOldImages bool   `json:"imagePollDeleteOldImages"`
}

type updateApplicationRequest struct {
	Name                     string `json:"name" binding:"required"`
	Icon                     string `json:"icon" binding:"omitempty,min=1,max=128"`
	RepositoryId             string `json:"repositoryId" binding:"required"`
	AgentId                  string `json:"agentId" binding:"required"`
	Branch                   string `json:"branch" binding:"required"`
	Path                     string `json:"path" binding:"required"`
	ImagePollEnabled         bool   `json:"imagePollEnabled"`
	ImagePollIntervalSeconds int64  `json:"imagePollIntervalSeconds"`
	ImagePollDeleteOldImages bool   `json:"imagePollDeleteOldImages"`
}

type applicationListResponse struct {
	Id             string  `json:"id"`
	Icon           string  `json:"icon"`
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
	Id                       string  `json:"id"`
	Icon                     string  `json:"icon"`
	Name                     string  `json:"name"`
	RepositoryId             string  `json:"repositoryId"`
	RepositoryName           string  `json:"repositoryName"`
	RepositoryUrl            string  `json:"repositoryUrl"`
	AgentId                  string  `json:"agentId"`
	AgentName                string  `json:"agentName"`
	SyncStatus               string  `json:"syncStatus"`
	HealthStatus             string  `json:"healthStatus"`
	Branch                   string  `json:"branch"`
	Commit                   string  `json:"commit"`
	CommitMessage            string  `json:"commitMessage"`
	LastSyncedAt             *string `json:"lastSyncedAt"`
	LastSyncError            *string `json:"lastSyncError,omitempty"`
	Path                     string  `json:"path"`
	CreatedAt                string  `json:"createdAt"`
	UpdatedAt                string  `json:"updatedAt"`
	ComposeFile              string  `json:"composeFile"`
	PreviousComposeFile      string  `json:"previousComposeFile,omitempty"`
	ImagePollEnabled         bool    `json:"imagePollEnabled"`
	ImagePollIntervalSeconds int64   `json:"imagePollIntervalSeconds"`
	ImagePollDeleteOldImages bool    `json:"imagePollDeleteOldImages"`
	ImageWebhookEnabled      bool    `json:"imageWebhookEnabled"`
	ImageWebhookUrl          *string `json:"imageWebhookUrl,omitempty"`
}

// Represents the many-to-many relationship between applications and notifications
type ApplicationNotification struct {
	ApplicationId  string `gorm:"primaryKey"`
	NotificationId string `gorm:"primaryKey"`
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

// applicationNameTaken reports whether another application already uses name
// (case-insensitive). Names are stored encrypted with a random nonce, so a DB
// unique index can't enforce this — we decrypt and compare in code. excludeID
// skips a record (the one being updated).
func applicationNameTaken(ctx context.Context, name, excludeID string) (bool, error) {
	apps, err := gorm.G[models.Application](db.DB).Find(ctx)
	if err != nil {
		return false, err
	}
	target := strings.TrimSpace(name)
	for i := range apps {
		if apps[i].Id == excludeID {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(apps[i].Name.String()), target) {
			return true, nil
		}
	}
	return false, nil
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

	nameTaken, err := applicationNameTaken(c.Request.Context(), req.Name, "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if nameTaken {
		c.JSON(http.StatusConflict, gin.H{"error": "an application with this name already exists"})
		return
	}

	composeFile, latestCommit, statusCode, sourceErr := fetchApplicationComposeAndCommit(c, req.RepositoryId, req.Branch, req.Path)
	if sourceErr != "" {
		c.JSON(statusCode, gin.H{"error": sourceErr})
		return
	}

	application := models.Application{
		Name:                     crypto.EncryptedString(req.Name),
		Icon:                     defaultString(req.Icon, defaultApplicationIcon),
		RepositoryId:             req.RepositoryId,
		AgentId:                  req.AgentId,
		SyncStatus:               models.UnknownSync,
		HealthStatus:             models.UnknownHealth,
		Branch:                   req.Branch,
		Commit:                   latestCommit.Hash,
		CommitMessage:            latestCommit.Message,
		LastSyncedAt:             nil,
		Path:                     req.Path,
		ComposeFile:              crypto.EncryptedString(composeFile),
		PreviousComposeFile:      crypto.EncryptedString(""),
		ImagePollEnabled:         req.ImagePollEnabled,
		ImagePollIntervalSeconds: req.ImagePollIntervalSeconds,
		ImagePollDeleteOldImages: req.ImagePollDeleteOldImages,
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

	utils.RecordAuditLog(c, "created", "application", application.Id)

	c.JSON(http.StatusCreated, toApplicationResponse(&createdApplication))
	sse.PublishUpdate(ApplicationsPath)

	var notifications []models.Notification
	if err := db.DB.Where("enable_by_default = ?", true).Find(&notifications).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	for _, notification := range notifications {
		association := ApplicationNotification{
			ApplicationId:  application.Id,
			NotificationId: notification.Id,
		}
		if err := db.DB.Create(&association).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to associate notifications"})
			return
		}
	}

	sendAgentSettings(c, req.AgentId)

	if application_deployer.DefaultApplicationDeployer != nil {
		_ = application_deployer.DefaultApplicationDeployer.TriggerApplicationDeploy(c, &createdApplication, composeFile)
	}
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

	nameTaken, err := applicationNameTaken(c.Request.Context(), req.Name, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if nameTaken {
		c.JSON(http.StatusConflict, gin.H{"error": "an application with this name already exists"})
		return
	}

	composeFile, latestCommit, statusCode, sourceErr := fetchApplicationComposeAndCommit(c, req.RepositoryId, req.Branch, req.Path)
	if sourceErr != "" {
		c.JSON(statusCode, gin.H{"error": sourceErr})
		return
	}

	oldAgentId := application.AgentId
	application.Name = crypto.EncryptedString(req.Name)
	if req.Icon != "" {
		application.Icon = req.Icon
	}
	application.RepositoryId = req.RepositoryId
	application.AgentId = req.AgentId
	application.Branch = req.Branch
	application.Commit = latestCommit.Hash
	application.CommitMessage = latestCommit.Message
	application.Path = req.Path
	application.PreviousComposeFile = application.ComposeFile
	application.ComposeFile = crypto.EncryptedString(composeFile)
	application.ImagePollEnabled = req.ImagePollEnabled
	application.ImagePollIntervalSeconds = req.ImagePollIntervalSeconds
	application.ImagePollDeleteOldImages = req.ImagePollDeleteOldImages

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

	utils.RecordAuditLog(c, "updated", "application", id)

	c.JSON(http.StatusOK, toApplicationResponse(&updatedApplication))
	sse.PublishUpdate(ApplicationsPath)
	sendAgentSettings(c, req.AgentId)
	if oldAgentId != req.AgentId {
		sendAgentSettings(c, oldAgentId)
	}
}

func DeleteApplicationHandler(c *gin.Context) {
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

	if websocket.DefaultHub == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "hub not initialized"})
		return
	}

	// Tear down on the agent first, and only delete our record once the agent
	// confirms. This keeps the hub from losing track of still-running containers
	// when the agent is offline or the removal fails.
	ctx, cancel := context.WithTimeout(c.Request.Context(), agentDeleteTimeout)
	defer cancel()

	result, err := websocket.DefaultHub.RemoveApplication(ctx, application.AgentId, application.Id, application.Name.String())
	if err != nil {
		if errors.Is(err, websocket.ErrAgentOffline) {
			c.JSON(http.StatusConflict, gin.H{"error": "agent is offline; cannot delete application while its containers may still be running"})
			return
		}
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": "agent did not respond to the delete request"})
		return
	}
	if !result.Success {
		c.JSON(http.StatusBadGateway, gin.H{"error": "agent failed to remove application: " + result.ErrorMessage})
		return
	}

	rowsAffected, err := gorm.G[models.Application](db.DB).Where("id = ?", id).Delete(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "application not found"})
		return
	}

	utils.RecordAuditLog(c, "deleted", "application", id)

	c.JSON(http.StatusOK, gin.H{"message": "application deleted"})
	sse.PublishUpdate(ApplicationsPath)
	sendAgentSettings(c, application.AgentId)
}

func DeployApplicationHandler(c *gin.Context) {
	if application_deployer.DefaultApplicationDeployer == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "application deployer not initialized"})
		return
	}

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

	err = application_deployer.DefaultApplicationDeployer.TriggerApplicationDeploy(c, &application, application.ComposeFile.String())

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to trigger application deploy: %v", err)})
		return
	} else {
		c.JSON(http.StatusAccepted, gin.H{"message": "deployment started"})
	}

	sse.PublishUpdate(ApplicationsPath)
}

func toApplicationListResponse(app *models.Application) applicationListResponse {
	return applicationListResponse{
		Id:             app.Id,
		Icon:           app.Icon,
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

// normalizeSyncError returns nil for a missing or empty error so the JSON field is
// omitted (cleared errors are stored as "").
func normalizeSyncError(e *string) *string {
	if e == nil || *e == "" {
		return nil
	}
	return e
}

func toApplicationResponse(app *models.Application) applicationResponse {
	resp := applicationResponse{
		Id:                       app.Id,
		Icon:                     app.Icon,
		Name:                     app.Name.String(),
		RepositoryId:             app.RepositoryId,
		RepositoryName:           app.Repository.Name,
		RepositoryUrl:            app.Repository.Url,
		AgentId:                  app.AgentId,
		AgentName:                app.Agent.Name.String(),
		SyncStatus:               string(app.SyncStatus),
		HealthStatus:             string(app.HealthStatus),
		Branch:                   app.Branch,
		Commit:                   app.Commit,
		CommitMessage:            app.CommitMessage,
		LastSyncedAt:             formatTimestamp(app.LastSyncedAt),
		LastSyncError:            normalizeSyncError(app.LastSyncError),
		Path:                     app.Path,
		CreatedAt:                app.CreatedAt.Format(time.RFC3339),
		UpdatedAt:                app.UpdatedAt.Format(time.RFC3339),
		ComposeFile:              app.ComposeFile.String(),
		PreviousComposeFile:      app.PreviousComposeFile.String(),
		ImagePollEnabled:         app.ImagePollEnabled,
		ImagePollIntervalSeconds: app.ImagePollIntervalSeconds,
		ImagePollDeleteOldImages: app.ImagePollDeleteOldImages,
		ImageWebhookEnabled:      app.ImageWebhookSecret != nil,
	}
	if app.ImageWebhookSecret != nil {
		webhookUrl := fmt.Sprintf("%s/api/v1/webhooks/images/%s", appUrl, app.Id)
		resp.ImageWebhookUrl = &webhookUrl
	}
	return resp
}

// sendAgentSettings fetches all applications for agentID and pushes an
// AgentSettings message to that agent if it is currently connected.
func sendAgentSettings(c *gin.Context, agentID string) {
	if websocket.DefaultHub == nil {
		return
	}
	apps, err := gorm.G[models.Application](db.DB).Where("agent_id = ?", agentID).Find(c.Request.Context())
	if err != nil {
		return
	}
	websocket.DefaultHub.SendAgentSettings(agentID, apps)
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
