package routes

import (
	"context"
	"errors"
	"net/http"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	application_deployer "github.com/OrcaCD/orca-cd/internal/hub/deployer"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/OrcaCD/orca-cd/internal/hub/rollback"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/danielgtaylor/huma/v2"
	"gorm.io/gorm"
)

// RollbackApplicationHandler deploys an earlier deployment of the application
// again. Automatic syncs are paused afterwards, so they don't undo it.
var RollbackApplicationHandler = restapi.Handler(huma.Operation{
	OperationID:   "rollback-application",
	Method:        http.MethodPost,
	Path:          "/applications/{id}/deployments/{deploymentId}/rollback",
	DefaultStatus: http.StatusAccepted,
	Summary:       "Roll back an application",
	Description: "Deploys an earlier successful deployment again, with the exact images it ran. " +
		"Automatic syncs are paused afterwards, so they don't undo it. " +
		requires(models.ResourceApplication, models.ActionRollback),
	Tags:     []string{tagApplications},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusNotFound, http.StatusConflict, http.StatusForbidden},
}, func(ctx context.Context, input *struct {
	ID           string `path:"id" doc:"The ID of the application"`
	DeploymentID string `path:"deploymentId" doc:"The ID of the successful deployment to roll back to"`
}) (*restapi.MessageOutput, error) {
	c := restapi.GinContext(ctx)
	id := input.ID

	application, err := gorm.G[models.Application](db.DB).
		Preload("Agent", nil).
		Where("id = ?", id).
		First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("application not found")
		}
		return nil, internalError()
	}

	target, err := gorm.G[models.ApplicationDeployment](db.DB).
		Where("id = ? AND application_id = ? AND status = ?", input.DeploymentID, id, models.DeploymentSucceeded).
		First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("deployment not found")
		}
		return nil, internalError()
	}

	if application.SyncStatus == models.Syncing {
		return nil, huma.Error409Conflict("a deployment is already in progress")
	}
	if application.CurrentDeploymentId != nil && *application.CurrentDeploymentId == target.Id {
		return nil, huma.Error409Conflict("the application already runs this deployment")
	}

	actorUserID, actorName := eventActor(c)
	err = rollback.Trigger(ctx, &application, &target, rollback.Origin{
		Source:      models.ApplicationEventSourceManual,
		ActorUserID: actorUserID,
		ActorName:   actorName,
	})
	if err != nil {
		if errors.Is(err, application_deployer.ErrAgentOffline) {
			return nil, huma.Error409Conflict("agent is offline; cannot roll back application")
		}
		if errors.Is(err, application_deployer.ErrAgentFileSyncUnsupported) {
			return nil, huma.Error409Conflict(err.Error())
		}
		return nil, huma.Error500InternalServerError("failed to trigger rollback")
	}

	utils.RecordAuditLog(c, "rolled-back", "application", id)
	sse.PublishUpdate(sse.TopicApplications.For(id))
	return restapi.Message("rollback started"), nil
})

// ResumeApplicationAutoSyncHandler resumes automatic syncs of a rolled back
// application. It keeps running the deployment it was rolled back to until a
// new commit is deployed.
var ResumeApplicationAutoSyncHandler = restapi.Handler(huma.Operation{
	OperationID: "resume-application-auto-sync",
	Method:      http.MethodPost,
	Path:        "/applications/{id}/resume-auto-sync",
	Summary:     "Resume the automatic syncs of an application",
	Description: "Resumes the automatic syncs paused by a rollback. The application keeps running the deployment " +
		"it was rolled back to until a new commit is deployed. " +
		requires(models.ResourceApplication, models.ActionRollback),
	Tags:     []string{tagApplications},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusNotFound, http.StatusForbidden},
}, func(ctx context.Context, input *idInput) (*restapi.MessageOutput, error) {
	c := restapi.GinContext(ctx)
	id := input.ID

	rowsAffected, err := gorm.G[models.Application](db.DB).
		Where("id = ? AND auto_sync_paused = ?", id, true).
		Update(ctx, "auto_sync_paused", false)
	if err != nil {
		return nil, internalError()
	}
	if rowsAffected == 0 {
		exists, err := hasRecord[models.Application](c, id)
		if err != nil {
			return nil, internalError()
		}
		if !exists {
			return nil, huma.Error404NotFound("application not found")
		}
		return restapi.Message("auto-sync is not paused"), nil
	}

	utils.RecordAuditLog(c, "resumed-auto-sync", "application", id)
	sse.PublishUpdate(sse.TopicApplications.For(id))
	return restapi.Message("auto-sync resumed"), nil
})

const (
	minRollbackHealthTimeoutSeconds = 30
	maxRollbackHealthTimeoutSeconds = 60 * 60
)

// updateRollbackSettingsRequest replaces all rollback settings, so every
// field is required: an omitted one would be reset.
type updateRollbackSettingsRequest struct {
	Enabled              bool  `json:"enabled" required:"true" doc:"Whether a failed deployment is rolled back automatically"`
	OnDeployFailure      bool  `json:"onDeployFailure" required:"true" doc:"Roll back when the deployment fails"`
	OnUnhealthy          bool  `json:"onUnhealthy" required:"true" doc:"Roll back when the deployment doesn't become healthy in time"`
	HealthTimeoutSeconds int64 `json:"healthTimeoutSeconds" required:"true" doc:"How long a deployment may take to become healthy, between 30 and 3600"`
	PauseAutoSync        bool  `json:"pauseAutoSync" required:"true" doc:"Pause automatic syncs after a rollback, so they don't undo it"`
}

// UpdateRollbackSettingsHandler configures the automatic rollback of an
// application.
var UpdateRollbackSettingsHandler = restapi.Handler(huma.Operation{
	OperationID: "update-application-rollback-settings",
	Method:      http.MethodPut,
	Path:        "/applications/{id}/rollback/settings",
	Summary:     "Update the rollback settings of an application",
	Description: "Replaces all settings of the automatic rollback. " + requires(models.ResourceApplication, models.ActionUpdate),
	Tags:        []string{tagApplications},
	Security:    restapi.Authenticated,
	Errors:      []int{http.StatusBadRequest, http.StatusNotFound, http.StatusForbidden},
}, func(ctx context.Context, input *struct {
	ID   string `path:"id" doc:"The ID of the application"`
	Body updateRollbackSettingsRequest
}) (*applicationOutput, error) {
	c := restapi.GinContext(ctx)
	id, req := input.ID, input.Body

	if req.HealthTimeoutSeconds < minRollbackHealthTimeoutSeconds || req.HealthTimeoutSeconds > maxRollbackHealthTimeoutSeconds {
		return nil, huma.Error400BadRequest("health timeout must be between 30 seconds and 1 hour")
	}

	rowsAffected, err := gorm.G[models.Application](db.DB).
		Where("id = ?", id).
		Select("RollbackEnabled", "RollbackOnDeployFailure", "RollbackOnUnhealthy", "RollbackHealthTimeoutSeconds", "RollbackPauseAutoSync").
		Updates(ctx, models.Application{
			RollbackEnabled:              req.Enabled,
			RollbackOnDeployFailure:      req.OnDeployFailure,
			RollbackOnUnhealthy:          req.OnUnhealthy,
			RollbackHealthTimeoutSeconds: req.HealthTimeoutSeconds,
			RollbackPauseAutoSync:        req.PauseAutoSync,
		})
	if err != nil {
		return nil, internalError()
	}
	if rowsAffected == 0 {
		return nil, huma.Error404NotFound("application not found")
	}

	application, err := gorm.G[models.Application](db.DB).
		Preload("Repository", nil).
		Preload("Agent", nil).
		Where("id = ?", id).
		First(ctx)
	if err != nil {
		return nil, internalError()
	}

	utils.RecordAuditLog(c, "updated", "application", id)
	sse.PublishUpdate(sse.TopicApplications.For(id))
	// The images kept for rollbacks depend on the settings.
	sendAgentSettings(c, application.AgentId)

	return &applicationOutput{Body: toApplicationResponse(&application)}, nil
})
