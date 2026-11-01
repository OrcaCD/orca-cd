package routes

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/applicationevents"
	hubApplications "github.com/OrcaCD/orca-cd/internal/hub/applications"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	application_deployer "github.com/OrcaCD/orca-cd/internal/hub/deployer"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/repositories"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/OrcaCD/orca-cd/internal/hub/websocket"
	sharedutils "github.com/OrcaCD/orca-cd/internal/shared/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const defaultApplicationIcon = "box"

// agentDeleteTimeout matches the agent's compose-down timeout (deploymentTimeout)
// so the hub waits for teardown to finish instead of timing out early and
// leaving its record out of sync with the agent.
const agentDeleteTimeout = 5 * time.Minute

// minImagePollIntervalSeconds mirrors the agent's behavior, so a shorter interval is
// rejected instead of being silently raised.
const minImagePollIntervalSeconds = 60

func validImagePollInterval(enabled bool, seconds int64) bool {
	if enabled {
		return seconds >= minImagePollIntervalSeconds
	}
	return seconds >= 0
}

type createApplicationRequest struct {
	Name                     string `json:"name" binding:"required"`
	Icon                     string `json:"icon" binding:"omitempty,min=1,max=128"`
	RepositoryId             string `json:"repositoryId" binding:"required"`
	AgentId                  string `json:"agentId" binding:"required"`
	Branch                   string `json:"branch" binding:"required"`
	Path                     string `json:"path" binding:"required"`
	ImagePollEnabled         bool   `json:"imagePollEnabled"`
	ImagePollIntervalSeconds int64  `json:"imagePollIntervalSeconds"`
	DeleteOldImages          bool   `json:"deleteOldImages"`
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
	DeleteOldImages          bool   `json:"deleteOldImages"`
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
	LastSyncError  *string `json:"lastSyncError,omitempty"`
	AgentId        string  `json:"agentId"`
	AgentName      string  `json:"agentName"`
	RepositoryId   string  `json:"repositoryId"`
	RepositoryName string  `json:"repositoryName"`
}

type applicationResponse struct {
	Id                       string  `json:"id"`
	Icon                     string  `json:"icon"`
	Name                     string  `json:"name"`
	RepositoryId             string  `json:"repositoryId"`
	RepositoryName           string  `json:"repositoryName"`
	RepositoryUrl            string  `json:"repositoryUrl"`
	RepositorySyncType       string  `json:"repositorySyncType"`
	AgentId                  string  `json:"agentId"`
	AgentName                string  `json:"agentName"`
	SyncStatus               string  `json:"syncStatus"`
	HealthStatus             string  `json:"healthStatus"`
	Branch                   string  `json:"branch"`
	Commit                   string  `json:"commit"`
	CommitMessage            string  `json:"commitMessage"`
	LastSyncedAt             *string `json:"lastSyncedAt"`
	LastSyncError            *string `json:"lastSyncError,omitempty"`
	LastSyncSource           *string `json:"lastSyncSource,omitempty"`
	Path                     string  `json:"path"`
	CreatedAt                string  `json:"createdAt"`
	UpdatedAt                string  `json:"updatedAt"`
	ComposeFile              string  `json:"composeFile"`
	PreviousComposeFile      string  `json:"previousComposeFile,omitempty"`
	ImagePollEnabled         bool    `json:"imagePollEnabled"`
	ImagePollIntervalSeconds int64   `json:"imagePollIntervalSeconds"`
	DeleteOldImages          bool    `json:"deleteOldImages"`
	CurrentDeploymentId      *string `json:"currentDeploymentId"`
	ImageWebhookEnabled      bool    `json:"imageWebhookEnabled"`
	ImageWebhookUrl          *string `json:"imageWebhookUrl,omitempty"`
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
	if !validImagePollInterval(req.ImagePollEnabled, req.ImagePollIntervalSeconds) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "image poll interval must be at least 60 seconds"})
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
		Name:                     crypto.EncryptedString(req.Name),
		NameHash:                 crypto.BlindIndex(models.NormalizeName(req.Name)),
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
		DeleteOldImages:          req.DeleteOldImages,
	}

	if err := gorm.G[models.Application](db.DB).Select("*").Create(c.Request.Context(), &application); err != nil {
		if errors.Is(err, gorm.ErrForeignKeyViolated) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid repositoryId or agentId"})
			return
		}
		if isUniqueConstraintError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "an application with this name already exists"})
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
	sse.PublishUpdate(sse.TopicApplications)

	sendAgentSettings(c, req.AgentId)

	if application_deployer.DefaultApplicationDeployer != nil {
		requestID := uuid.NewString()
		actorUserID, actorName := eventActor(c)
		if _, err := applicationevents.Start(c.Request.Context(), applicationevents.Params{
			ApplicationID: createdApplication.Id,
			RequestID:     &requestID,
			Type:          models.ApplicationEventDeployment,
			Source:        models.ApplicationEventSourceApplicationCreated,
			ActorUserID:   actorUserID,
			ActorName:     actorName,
		}); err != nil {
			hubApplications.Log.Error().Err(err).Str("applicationId", createdApplication.Id).Msg("failed to record initial deployment event")
		}
		_ = application_deployer.DefaultApplicationDeployer.TriggerApplicationDeploy(c, &createdApplication, composeFile, requestID)
	}
}

func UpdateApplicationHandler(c *gin.Context) {
	id := c.Param("id")

	var req updateApplicationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: name, repositoryId, agentId, syncStatus, healthStatus, branch, commit, commitMessage, and path are required"})
		return
	}
	if !validImagePollInterval(req.ImagePollEnabled, req.ImagePollIntervalSeconds) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "image poll interval must be at least 60 seconds"})
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

	nameHash := crypto.BlindIndex(models.NormalizeName(req.Name))
	oldAgentId := application.AgentId
	// Moving to another agent or renaming to a different compose project would
	// leave the old containers running, so tear down the old deployment first.
	if oldAgentId != req.AgentId || composeProjectChanged(application.Name.String(), req.Name) {
		// Check the name is free on the target agent before tearing down the old
		// deployment, so a conflict doesn't leave the application undeployed.
		conflicts, err := gorm.G[models.Application](db.DB).
			Where("agent_id = ? AND name_hash = ? AND id <> ?", req.AgentId, nameHash, id).
			Count(c.Request.Context(), "*")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			return
		}
		if conflicts > 0 {
			c.JSON(http.StatusConflict, gin.H{"error": "an application with this name already exists"})
			return
		}

		if status, msg := undeployApplication(c.Request.Context(), &application, "move or rename"); status != 0 {
			c.JSON(status, gin.H{"error": msg})
			return
		}
	}

	application.Name = crypto.EncryptedString(req.Name)
	application.NameHash = nameHash
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
	application.DeleteOldImages = req.DeleteOldImages

	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", id).
		Select("*").
		Omit("SyncStatus", "HealthStatus", "LastSyncedAt", "LastSyncError", "LastSyncSource", "CurrentDeploymentId").
		Updates(c.Request.Context(), application); err != nil {
		if errors.Is(err, gorm.ErrForeignKeyViolated) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid repositoryId or agentId"})
			return
		}
		if isUniqueConstraintError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "an application with this name already exists"})
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
	sse.PublishUpdate(sse.TopicApplications)
	sendAgentSettings(c, req.AgentId)
	if oldAgentId != req.AgentId {
		sendAgentSettings(c, oldAgentId)
	}
}

// composeProjectChanged reports whether renaming an application changes the
// compose project name the agent deploys it under.
func composeProjectChanged(oldName, newName string) bool {
	oldProject, err := sharedutils.NormalizeProjectName(oldName)
	if err != nil {
		// The agent could never have deployed under the old name.
		return false
	}
	newProject, _ := sharedutils.NormalizeProjectName(newName)
	return oldProject != newProject
}

// undeployApplication removes the application from its agent and waits for
// confirmation. Returns a zero status on success, else the HTTP status and message.
func undeployApplication(ctx context.Context, application *models.Application, action string) (int, string) {
	if websocket.DefaultHub == nil {
		return http.StatusInternalServerError, "hub not initialized"
	}

	ctx, cancel := context.WithTimeout(ctx, agentDeleteTimeout)
	defer cancel()

	result, err := websocket.DefaultHub.RemoveApplication(ctx, application.AgentId, application.Id, application.Name.String())
	if err != nil {
		if errors.Is(err, websocket.ErrAgentOffline) {
			return http.StatusConflict, "agent is offline; cannot " + action + " application while its containers may still be running"
		}
		return http.StatusGatewayTimeout, "agent did not respond to the delete request"
	}
	if !result.Success {
		return http.StatusBadGateway, "agent failed to remove application: " + result.ErrorMessage
	}

	return 0, ""
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

	if status, msg := undeployApplication(c.Request.Context(), &application, "delete"); status != 0 {
		c.JSON(status, gin.H{"error": msg})
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
	sse.PublishUpdate(sse.TopicApplications)
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

	requestID := uuid.NewString()
	actorUserID, actorName := eventActor(c)
	if _, eventErr := applicationevents.Start(c.Request.Context(), applicationevents.Params{
		ApplicationID: application.Id,
		RequestID:     &requestID,
		Type:          models.ApplicationEventDeployment,
		Source:        models.ApplicationEventSourceManual,
		ActorUserID:   actorUserID,
		ActorName:     actorName,
	}); eventErr != nil {
		hubApplications.Log.Error().Err(eventErr).Str("applicationId", application.Id).Msg("failed to record manual deployment event")
	}

	err = application_deployer.DefaultApplicationDeployer.TriggerApplicationDeploy(c, &application, application.ComposeFile.String(), requestID)

	if err != nil {
		if errors.Is(err, application_deployer.ErrAgentOffline) {
			c.JSON(http.StatusConflict, gin.H{"error": "agent is offline; cannot deploy application"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to trigger application deploy: %v", err)})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "deployment started"})
	sse.PublishUpdate(sse.TopicApplications)
}

// PullApplicationImagesHandler asks the agent to pull newer images for the
// application and redeploy it when any image changed.
func PullApplicationImagesHandler(c *gin.Context) {
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

	actorUserID, actorName := eventActor(c)
	accepted := hubApplications.TriggerImagePull(&application, hubApplications.SyncOrigin{
		Source:      models.ApplicationEventSourceManual,
		ActorUserID: actorUserID,
		ActorName:   actorName,
	})
	if !accepted {
		c.JSON(http.StatusConflict, gin.H{"error": "agent is offline; cannot pull images"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "image pull started"})
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
		LastSyncError:  normalizeSyncError(app.LastSyncError),
		Path:           app.Path,
		RepositoryId:   app.RepositoryId,
		RepositoryName: app.Repository.Name,
		AgentId:        app.AgentId,
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

// normalizeSyncSource returns nil for a missing or empty source so the JSON field
// is omitted (syncs with an unknown trigger store "").
func normalizeSyncSource(source *models.ApplicationEventSource) *string {
	if source == nil || *source == "" {
		return nil
	}
	s := string(*source)
	return &s
}

func toApplicationResponse(app *models.Application) applicationResponse {
	resp := applicationResponse{
		Id:                       app.Id,
		Icon:                     app.Icon,
		Name:                     app.Name.String(),
		RepositoryId:             app.RepositoryId,
		RepositoryName:           app.Repository.Name,
		RepositoryUrl:            app.Repository.Url,
		RepositorySyncType:       string(app.Repository.SyncType),
		AgentId:                  app.AgentId,
		AgentName:                app.Agent.Name.String(),
		SyncStatus:               string(app.SyncStatus),
		HealthStatus:             string(app.HealthStatus),
		Branch:                   app.Branch,
		Commit:                   app.Commit,
		CommitMessage:            app.CommitMessage,
		LastSyncedAt:             formatTimestamp(app.LastSyncedAt),
		LastSyncError:            normalizeSyncError(app.LastSyncError),
		LastSyncSource:           normalizeSyncSource(app.LastSyncSource),
		Path:                     app.Path,
		CreatedAt:                app.CreatedAt.Format(time.RFC3339),
		UpdatedAt:                app.UpdatedAt.Format(time.RFC3339),
		ComposeFile:              app.ComposeFile.String(),
		PreviousComposeFile:      app.PreviousComposeFile.String(),
		ImagePollEnabled:         app.ImagePollEnabled,
		ImagePollIntervalSeconds: app.ImagePollIntervalSeconds,
		DeleteOldImages:          app.DeleteOldImages,
		CurrentDeploymentId:      app.CurrentDeploymentId,
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

	if err := repositories.VerifyCommit(c.Request.Context(), provider, &repo, latestCommit.Hash); err != nil {
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
