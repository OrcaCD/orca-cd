package routes

import (
	"context"
	"encoding/json"
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
	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/OrcaCD/orca-cd/internal/hub/websocket"
	"github.com/OrcaCD/orca-cd/internal/shared/composewarnings"
	sharedutils "github.com/OrcaCD/orca-cd/internal/shared/utils"
	"github.com/danielgtaylor/huma/v2"
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
	Name                     string `json:"name" required:"true" minLength:"1"`
	Icon                     string `json:"icon" maxLength:"128"`
	RepositoryId             string `json:"repositoryId" required:"true" minLength:"1"`
	AgentId                  string `json:"agentId" required:"true" minLength:"1"`
	Branch                   string `json:"branch" required:"true" minLength:"1"`
	Path                     string `json:"path" required:"true" minLength:"1" doc:"The path of the compose file in the repository"`
	ImagePollEnabled         bool   `json:"imagePollEnabled"`
	ImagePollIntervalSeconds int64  `json:"imagePollIntervalSeconds" doc:"At least 60 when image polling is enabled"`
	DeleteOldImages          bool   `json:"deleteOldImages"`
}

type updateApplicationRequest struct {
	Name                     string `json:"name" required:"true" minLength:"1"`
	Icon                     string `json:"icon" maxLength:"128" doc:"Leave it out to keep the icon"`
	RepositoryId             string `json:"repositoryId" required:"true" minLength:"1"`
	AgentId                  string `json:"agentId" required:"true" minLength:"1"`
	Branch                   string `json:"branch" required:"true" minLength:"1"`
	Path                     string `json:"path" required:"true" minLength:"1" doc:"The path of the compose file in the repository"`
	ImagePollEnabled         bool   `json:"imagePollEnabled"`
	ImagePollIntervalSeconds int64  `json:"imagePollIntervalSeconds" doc:"At least 60 when image polling is enabled"`
	DeleteOldImages          bool   `json:"deleteOldImages"`
}

type applicationListResponse struct {
	Id               string  `json:"id" required:"true"`
	Icon             string  `json:"icon" required:"true"`
	Name             string  `json:"name" required:"true"`
	HealthStatus     string  `json:"healthStatus" required:"true"`
	SyncStatus       string  `json:"syncStatus" required:"true"`
	Branch           string  `json:"branch" required:"true"`
	Commit           string  `json:"commit" required:"true"`
	LastSyncedAt     *string `json:"lastSyncedAt" required:"true" nullable:"true" format:"date-time"`
	Path             string  `json:"path" required:"true"`
	LastSyncError    *string `json:"lastSyncError,omitempty"`
	AgentId          string  `json:"agentId" required:"true"`
	AgentName        string  `json:"agentName" required:"true"`
	RepositoryId     string  `json:"repositoryId" required:"true"`
	RolledBackAt     *string `json:"rolledBackAt" required:"true" nullable:"true" format:"date-time" doc:"Set while the application runs a deployment it was rolled back to"`
	RolledBackCommit *string `json:"rolledBackCommit" required:"true" nullable:"true"`
	RepositoryName   string  `json:"repositoryName" required:"true"`
}

type applicationResponse struct {
	Id                       string                    `json:"id" required:"true"`
	Icon                     string                    `json:"icon" required:"true"`
	Name                     string                    `json:"name" required:"true"`
	RepositoryId             string                    `json:"repositoryId" required:"true"`
	RepositoryName           string                    `json:"repositoryName" required:"true"`
	RepositoryUrl            string                    `json:"repositoryUrl" required:"true"`
	RepositorySyncType       string                    `json:"repositorySyncType" required:"true"`
	AgentId                  string                    `json:"agentId" required:"true"`
	AgentName                string                    `json:"agentName" required:"true"`
	SyncStatus               string                    `json:"syncStatus" required:"true"`
	HealthStatus             string                    `json:"healthStatus" required:"true"`
	Branch                   string                    `json:"branch" required:"true"`
	Commit                   string                    `json:"commit" required:"true"`
	CommitMessage            string                    `json:"commitMessage" required:"true"`
	LastSyncedAt             *string                   `json:"lastSyncedAt" required:"true" nullable:"true" format:"date-time"`
	LastSyncError            *string                   `json:"lastSyncError,omitempty"`
	LastSyncSource           *string                   `json:"lastSyncSource,omitempty" doc:"What triggered the last sync"`
	Path                     string                    `json:"path" required:"true"`
	CreatedAt                string                    `json:"createdAt" required:"true" format:"date-time"`
	UpdatedAt                string                    `json:"updatedAt" required:"true" format:"date-time"`
	ComposeFile              string                    `json:"composeFile" required:"true"`
	PreviousComposeFile      string                    `json:"previousComposeFile,omitempty" doc:"The compose file before the last change of the application"`
	ImagePollEnabled         bool                      `json:"imagePollEnabled" required:"true"`
	ImagePollIntervalSeconds int64                     `json:"imagePollIntervalSeconds" required:"true"`
	DeleteOldImages          bool                      `json:"deleteOldImages" required:"true"`
	CurrentDeploymentId      *string                   `json:"currentDeploymentId" required:"true" nullable:"true"`
	RolledBackAt             *string                   `json:"rolledBackAt" required:"true" nullable:"true" format:"date-time" doc:"Set while the application runs a deployment it was rolled back to"`
	RolledBackCommit         *string                   `json:"rolledBackCommit" required:"true" nullable:"true"`
	RolledBackReason         *string                   `json:"rolledBackReason" required:"true" nullable:"true"`
	AutoSyncPaused           bool                      `json:"autoSyncPaused" required:"true" doc:"Automatic syncs are paused after a rollback until they are resumed"`
	Rollback                 rollbackSettingsResponse  `json:"rollback" required:"true"`
	ReleaseAge               releaseAgeSettings        `json:"releaseAge" required:"true"`
	ImageWebhookEnabled      bool                      `json:"imageWebhookEnabled" required:"true"`
	ImageWebhookUrl          *string                   `json:"imageWebhookUrl,omitempty"`
	Warnings                 []composewarnings.Warning `json:"warnings" required:"true" doc:"Problems found in the compose file"`
}

type rollbackSettingsResponse struct {
	Enabled              bool  `json:"enabled" required:"true"`
	OnDeployFailure      bool  `json:"onDeployFailure" required:"true"`
	OnUnhealthy          bool  `json:"onUnhealthy" required:"true"`
	HealthTimeoutSeconds int64 `json:"healthTimeoutSeconds" required:"true" doc:"How long a deployment may take to become healthy"`
	PauseAutoSync        bool  `json:"pauseAutoSync" required:"true" doc:"Whether automatic syncs are paused after a rollback"`
}

type applicationOutput struct {
	Body applicationResponse
}

var ListApplicationsHandler = restapi.Handler(huma.Operation{
	OperationID: "list-applications",
	Method:      http.MethodGet,
	Path:        "/applications",
	Summary:     "List applications",
	Description: "Lists the applications the user may view.",
	Tags:        []string{tagApplications},
	Security:    restapi.Authenticated,
}, func(ctx context.Context, _ *struct{}) (*struct{ Body []applicationListResponse }, error) {
	perms, err := permissionsFor(restapi.GinContext(ctx))
	if err != nil {
		return nil, err
	}

	applications, err := gorm.G[models.Application](db.DB).
		Scopes(perms.Scope(models.ResourceApplication, models.ActionRead, "id")).
		Preload("Repository", nil).
		Preload("Agent", nil).
		Order("created_at ASC").
		Find(ctx)
	if err != nil {
		return nil, internalError()
	}

	response := make([]applicationListResponse, 0, len(applications))
	for i := range applications {
		response = append(response, toApplicationListResponse(&applications[i]))
	}

	return &struct{ Body []applicationListResponse }{Body: response}, nil
})

var GetApplicationHandler = restapi.Handler(huma.Operation{
	OperationID: "get-application",
	Method:      http.MethodGet,
	Path:        "/applications/{id}",
	Summary:     "Get an application",
	Description: requires(models.ResourceApplication, models.ActionRead),
	Tags:        []string{tagApplications},
	Security:    restapi.Authenticated,
	Errors:      []int{http.StatusNotFound, http.StatusForbidden},
}, func(ctx context.Context, input *idInput) (*applicationOutput, error) {
	application, err := gorm.G[models.Application](db.DB).
		Preload("Repository", nil).
		Preload("Agent", nil).
		Where("id = ?", input.ID).
		First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("application not found")
		}
		return nil, internalError()
	}

	return &applicationOutput{Body: toApplicationResponse(&application)}, nil
})

var CreateApplicationHandler = restapi.Handler(huma.Operation{
	OperationID:   "create-application",
	Method:        http.MethodPost,
	Path:          "/applications",
	DefaultStatus: http.StatusCreated,
	Summary:       "Create an application",
	Description: "Reads the compose file from the latest commit of the branch and starts the first deployment. " +
		"Needs read access to the repository and the agent. Answers 422 when the repository can't be read. " +
		requires(models.ResourceApplication, models.ActionCreate),
	Tags:     []string{tagApplications},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusBadRequest, http.StatusForbidden, http.StatusConflict, http.StatusUnprocessableEntity},
}, func(ctx context.Context, input *struct{ Body createApplicationRequest }) (*applicationOutput, error) {
	c := restapi.GinContext(ctx)
	req := input.Body
	if !validImagePollInterval(req.ImagePollEnabled, req.ImagePollIntervalSeconds) {
		return nil, huma.Error400BadRequest("image poll interval must be at least 60 seconds")
	}
	if err := checkApplicationSources(c, req.RepositoryId, req.AgentId); err != nil {
		return nil, err
	}

	repoExists, err := hasRecord[models.Repository](c, req.RepositoryId)
	if err != nil {
		return nil, internalError()
	}
	if !repoExists {
		return nil, huma.Error400BadRequest("repository not found")
	}

	agentExists, err := hasRecord[models.Agent](c, req.AgentId)
	if err != nil {
		return nil, internalError()
	}
	if !agentExists {
		return nil, huma.Error400BadRequest("agent not found")
	}

	composeFile, latestCommit, err := fetchApplicationComposeAndCommit(ctx, req.RepositoryId, req.Branch, req.Path)
	if err != nil {
		return nil, err
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
		Warnings:                 "[]",
		PreviousComposeFile:      crypto.EncryptedString(""),
		ImagePollEnabled:         req.ImagePollEnabled,
		ImagePollIntervalSeconds: req.ImagePollIntervalSeconds,
		DeleteOldImages:          req.DeleteOldImages,
	}

	if err := gorm.G[models.Application](db.DB).Select("*").Create(ctx, &application); err != nil {
		if errors.Is(err, gorm.ErrForeignKeyViolated) {
			return nil, huma.Error400BadRequest("invalid repositoryId or agentId")
		}
		if isUniqueConstraintError(err) {
			return nil, huma.Error409Conflict("an application with this name already exists")
		}
		return nil, internalError()
	}

	createdApplication, err := gorm.G[models.Application](db.DB).
		Preload("Repository", nil).
		Preload("Agent", nil).
		Where("id = ?", application.Id).
		First(ctx)
	if err != nil {
		return nil, internalError()
	}

	utils.RecordAuditLog(c, "created", "application", application.Id)
	sse.PublishUpdate(sse.TopicApplications.For(createdApplication.Id))

	sendAgentSettings(c, req.AgentId)

	if application_deployer.DefaultApplicationDeployer != nil {
		requestID := uuid.NewString()
		actorUserID, actorName := eventActor(c)
		if _, err := applicationevents.Start(ctx, applicationevents.Params{
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

	return &applicationOutput{Body: toApplicationResponse(&createdApplication)}, nil
})

var UpdateApplicationHandler = restapi.Handler(huma.Operation{
	OperationID: "update-application",
	Method:      http.MethodPut,
	Path:        "/applications/{id}",
	Summary:     "Update an application",
	Description: "Reads the compose file from the latest commit of the branch again. Moving the application to another agent " +
		"or renaming its compose project removes it from the old agent first, which needs the `application:deploy` permission, " +
		"as does a new source, which also needs read access to the new repository or agent. " +
		"Answers 422 when the repository can't be read. " +
		requires(models.ResourceApplication, models.ActionUpdate),
	Tags:     []string{tagApplications},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity},
}, func(ctx context.Context, input *struct {
	ID   string `path:"id" doc:"The ID of the application"`
	Body updateApplicationRequest
}) (*applicationOutput, error) {
	c := restapi.GinContext(ctx)
	id, req := input.ID, input.Body
	if !validImagePollInterval(req.ImagePollEnabled, req.ImagePollIntervalSeconds) {
		return nil, huma.Error400BadRequest("image poll interval must be at least 60 seconds")
	}

	application, err := gorm.G[models.Application](db.DB).Where("id = ?", id).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("application not found")
		}
		return nil, internalError()
	}

	// Only a new source needs read access, so the settings of an application
	// can be changed without access to its repository and agent.
	repositoryId, agentId := req.RepositoryId, req.AgentId
	if repositoryId == application.RepositoryId && req.Branch == application.Branch && req.Path == application.Path {
		repositoryId = ""
	}
	if agentId == application.AgentId {
		agentId = ""
	}
	if err := checkApplicationSources(c, repositoryId, agentId); err != nil {
		return nil, err
	}
	// A new source or compose project replaces what is deployed
	if repositoryId != "" || agentId != "" || composeProjectChanged(application.Name.String(), req.Name) {
		perms, err := permissionsFor(c)
		if err != nil {
			return nil, err
		}
		if !perms.Can(models.ResourceApplication, models.ActionDeploy, id) {
			return nil, huma.Error403Forbidden("insufficient permissions to deploy the application")
		}
	}

	repoExists, err := hasRecord[models.Repository](c, req.RepositoryId)
	if err != nil {
		return nil, internalError()
	}
	if !repoExists {
		return nil, huma.Error400BadRequest("repository not found")
	}

	agentExists, err := hasRecord[models.Agent](c, req.AgentId)
	if err != nil {
		return nil, internalError()
	}
	if !agentExists {
		return nil, huma.Error400BadRequest("agent not found")
	}

	composeFile, latestCommit, err := fetchApplicationComposeAndCommit(ctx, req.RepositoryId, req.Branch, req.Path)
	if err != nil {
		return nil, err
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
			Count(ctx, "*")
		if err != nil {
			return nil, internalError()
		}
		if conflicts > 0 {
			return nil, huma.Error409Conflict("an application with this name already exists")
		}

		if status, msg := undeployApplication(ctx, &application, "move or rename"); status != 0 {
			return nil, huma.NewError(status, msg)
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
	if application.ComposeFile.String() != composeFile {
		application.Warnings = "[]"
	}
	application.PreviousComposeFile = application.ComposeFile
	application.ComposeFile = crypto.EncryptedString(composeFile)
	application.ImagePollEnabled = req.ImagePollEnabled
	application.ImagePollIntervalSeconds = req.ImagePollIntervalSeconds
	application.DeleteOldImages = req.DeleteOldImages

	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", id).
		Select("*").
		Omit("SyncStatus", "HealthStatus", "LastSyncedAt", "LastSyncError", "LastSyncSource",
			"CurrentDeploymentId", "RolledBackAt", "RolledBackCommit", "RolledBackReason", "AutoSyncPaused",
			"RollbackEnabled", "RollbackOnDeployFailure", "RollbackOnUnhealthy", "RollbackHealthTimeoutSeconds", "RollbackPauseAutoSync",
			"MinimumReleaseAgeSeconds", "ReleaseAgeAllowedRegistries", "ReleaseAgeServiceOverrides").
		Updates(ctx, application); err != nil {
		if errors.Is(err, gorm.ErrForeignKeyViolated) {
			return nil, huma.Error400BadRequest("invalid repositoryId or agentId")
		}
		if isUniqueConstraintError(err) {
			return nil, huma.Error409Conflict("an application with this name already exists")
		}
		return nil, internalError()
	}

	updatedApplication, err := gorm.G[models.Application](db.DB).
		Preload("Repository", nil).
		Preload("Agent", nil).
		Where("id = ?", id).
		First(ctx)
	if err != nil {
		return nil, internalError()
	}

	utils.RecordAuditLog(c, "updated", "application", id)
	sse.PublishUpdate(sse.TopicApplications.For(id))
	sendAgentSettings(c, req.AgentId)
	if oldAgentId != req.AgentId {
		sendAgentSettings(c, oldAgentId)
	}

	return &applicationOutput{Body: toApplicationResponse(&updatedApplication)}, nil
})

// checkApplicationSources checks that the user can read the repository and the
// agent an application is deployed from, since it exposes their files and names.
// An empty ID is not checked.
func checkApplicationSources(c *gin.Context, repositoryId, agentId string) error {
	perms, err := permissionsFor(c)
	if err != nil {
		return err
	}
	if repositoryId != "" && !perms.Can(models.ResourceRepository, models.ActionRead, repositoryId) {
		return huma.Error403Forbidden("insufficient permissions for the repository")
	}
	if agentId != "" && !perms.Can(models.ResourceAgent, models.ActionRead, agentId) {
		return huma.Error403Forbidden("insufficient permissions for the agent")
	}
	return nil
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

var DeleteApplicationHandler = restapi.Handler(huma.Operation{
	OperationID: "delete-application",
	Method:      http.MethodDelete,
	Path:        "/applications/{id}",
	Summary:     "Delete an application",
	Description: "Removes the application from its agent first and waits for it, so it fails while the agent is offline. " +
		requires(models.ResourceApplication, models.ActionDelete),
	Tags:     []string{tagApplications},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusNotFound, http.StatusConflict, http.StatusForbidden},
}, func(ctx context.Context, input *idInput) (*restapi.MessageOutput, error) {
	c := restapi.GinContext(ctx)
	id := input.ID

	application, err := gorm.G[models.Application](db.DB).Where("id = ?", id).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("application not found")
		}
		return nil, internalError()
	}

	if status, msg := undeployApplication(ctx, &application, "delete"); status != 0 {
		return nil, huma.NewError(status, msg)
	}

	rowsAffected, err := gorm.G[models.Application](db.DB).Where("id = ?", id).Delete(ctx)
	if err != nil {
		return nil, internalError()
	}
	if rowsAffected == 0 {
		return nil, huma.Error404NotFound("application not found")
	}

	utils.RecordAuditLog(c, "deleted", "application", id)

	sse.PublishUpdate(sse.TopicApplications)
	// After the update, so clients still get it before losing access
	pruneGrants(ctx)
	sendAgentSettings(c, application.AgentId)

	return restapi.Message("application deleted"), nil
})

var DeployApplicationHandler = restapi.Handler(huma.Operation{
	OperationID:   "deploy-application",
	Method:        http.MethodPost,
	Path:          "/applications/{id}/deploy",
	DefaultStatus: http.StatusAccepted,
	Summary:       "Deploy an application",
	Description: "Deploys the current compose file again. The deployment runs in the background, its progress shows in the events of the application. " +
		requires(models.ResourceApplication, models.ActionDeploy),
	Tags:     []string{tagApplications},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusNotFound, http.StatusConflict, http.StatusForbidden},
}, func(ctx context.Context, input *idInput) (*restapi.MessageOutput, error) {
	if application_deployer.DefaultApplicationDeployer == nil {
		return nil, huma.Error500InternalServerError("application deployer not initialized")
	}

	c := restapi.GinContext(ctx)
	id := input.ID
	application, err := gorm.G[models.Application](db.DB).
		Preload("Repository", nil).
		Preload("Agent", nil).
		Where("id = ?", id).
		First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("application not found")
		}
		return nil, internalError()
	}

	requestID := uuid.NewString()
	actorUserID, actorName := eventActor(c)
	if _, eventErr := applicationevents.Start(ctx, applicationevents.Params{
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
			return nil, huma.Error409Conflict("agent is offline; cannot deploy application")
		}
		return nil, huma.Error500InternalServerError(fmt.Sprintf("failed to trigger application deploy: %v", err))
	}

	sse.PublishUpdate(sse.TopicApplications.For(id))
	return restapi.Message("deployment started"), nil
})

// PullApplicationImagesHandler asks the agent to pull newer images for the
// application and redeploy it when any image changed.
var PullApplicationImagesHandler = restapi.Handler(huma.Operation{
	OperationID:   "pull-application-images",
	Method:        http.MethodPost,
	Path:          "/applications/{id}/pull-images",
	DefaultStatus: http.StatusAccepted,
	Summary:       "Pull the images of an application",
	Description: "Asks the agent to pull newer images and redeploy the application when any image changed. " +
		"Not possible while the application is rolled back. " +
		requires(models.ResourceApplication, models.ActionPullImages),
	Tags:     []string{tagApplications},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusNotFound, http.StatusConflict, http.StatusForbidden},
}, func(ctx context.Context, input *idInput) (*restapi.MessageOutput, error) {
	application, err := gorm.G[models.Application](db.DB).Where("id = ?", input.ID).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("application not found")
		}
		return nil, internalError()
	}

	if application.RolledBackAt != nil {
		return nil, huma.Error409Conflict("application is rolled back; deploy it again before pulling images")
	}

	actorUserID, actorName := eventActor(restapi.GinContext(ctx))
	accepted := hubApplications.TriggerImagePull(&application, hubApplications.SyncOrigin{
		Source:      models.ApplicationEventSourceManual,
		ActorUserID: actorUserID,
		ActorName:   actorName,
	})
	if !accepted {
		return nil, huma.Error409Conflict("agent is offline; cannot pull images")
	}

	return restapi.Message("image pull started"), nil
})

func toApplicationListResponse(app *models.Application) applicationListResponse {
	return applicationListResponse{
		Id:               app.Id,
		Icon:             app.Icon,
		Name:             app.Name.String(),
		HealthStatus:     string(app.HealthStatus),
		SyncStatus:       string(app.SyncStatus),
		Branch:           app.Branch,
		Commit:           app.Commit,
		LastSyncedAt:     formatTimestamp(app.LastSyncedAt),
		LastSyncError:    normalizeSyncError(app.LastSyncError),
		Path:             app.Path,
		RepositoryId:     app.RepositoryId,
		RepositoryName:   app.Repository.Name,
		AgentId:          app.AgentId,
		AgentName:        app.Agent.Name.String(),
		RolledBackAt:     formatTimestamp(app.RolledBackAt),
		RolledBackCommit: app.RolledBackCommit,
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
	var warnings []composewarnings.Warning
	if err := json.Unmarshal([]byte(app.Warnings), &warnings); err != nil {
		hubApplications.Log.Error().Err(err).Str("applicationId", app.Id).Msg("failed to decode application warnings")
		warnings = []composewarnings.Warning{}
	}
	if warnings == nil {
		warnings = []composewarnings.Warning{}
	}

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
		RolledBackAt:             formatTimestamp(app.RolledBackAt),
		RolledBackCommit:         app.RolledBackCommit,
		RolledBackReason:         app.RolledBackReason,
		AutoSyncPaused:           app.AutoSyncPaused,
		Rollback: rollbackSettingsResponse{
			Enabled:              app.RollbackEnabled,
			OnDeployFailure:      app.RollbackOnDeployFailure,
			OnUnhealthy:          app.RollbackOnUnhealthy,
			HealthTimeoutSeconds: app.RollbackHealthTimeoutSeconds,
			PauseAutoSync:        app.RollbackPauseAutoSync,
		},
		ReleaseAge:          toReleaseAgeSettings(app),
		ImageWebhookEnabled: app.ImageWebhookSecret != nil,
		Warnings:            warnings,
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
	ctx := c.Request.Context()
	apps, err := gorm.G[models.Application](db.DB).Where("agent_id = ?", agentID).Find(ctx)
	if err != nil {
		return
	}
	websocket.DefaultHub.SendAgentSettings(ctx, agentID, apps)
}

func fetchApplicationComposeAndCommit(ctx context.Context, repositoryID, branch, path string) (string, repositories.CommitInfo, error) {
	repo, err := gorm.G[models.Repository](db.DB).Where("id = ?", repositoryID).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", repositories.CommitInfo{}, huma.Error400BadRequest("repository not found")
		}
		return "", repositories.CommitInfo{}, internalError()
	}

	provider, err := repositories.Get(repo.Provider)
	if err != nil {
		return "", repositories.CommitInfo{}, huma.Error400BadRequest("unsupported provider")
	}

	latestCommit, err := provider.GetLatestCommit(ctx, &repo, branch)
	if err != nil {
		return "", repositories.CommitInfo{}, huma.Error422UnprocessableEntity(err.Error())
	}

	if err := repositories.VerifyCommit(ctx, provider, &repo, latestCommit.Hash); err != nil {
		return "", repositories.CommitInfo{}, huma.Error422UnprocessableEntity(err.Error())
	}

	composeFile, err := provider.GetFileContent(ctx, &repo, latestCommit.Hash, path)
	if err != nil {
		return "", repositories.CommitInfo{}, huma.Error422UnprocessableEntity(err.Error())
	}

	return composeFile, latestCommit, nil
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
