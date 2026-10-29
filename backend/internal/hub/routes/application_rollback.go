package routes

import (
	"errors"
	"net/http"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	application_deployer "github.com/OrcaCD/orca-cd/internal/hub/deployer"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/rollback"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// RollbackApplicationHandler deploys an earlier deployment of the application
// again. Automatic syncs are paused afterwards, so they don't undo it.
func RollbackApplicationHandler(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	application, err := gorm.G[models.Application](db.DB).
		Preload("Agent", nil).
		Where("id = ?", id).
		First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "application not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	target, err := gorm.G[models.ApplicationDeployment](db.DB).
		Where("id = ? AND application_id = ? AND status = ?", c.Param("deploymentId"), id, models.DeploymentSucceeded).
		First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "deployment not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	if application.SyncStatus == models.Syncing {
		c.JSON(http.StatusConflict, gin.H{"error": "a deployment is already in progress"})
		return
	}
	if application.CurrentDeploymentId != nil && *application.CurrentDeploymentId == target.Id {
		c.JSON(http.StatusConflict, gin.H{"error": "the application already runs this deployment"})
		return
	}

	actorUserID, actorName := eventActor(c)
	err = rollback.Trigger(ctx, &application, &target, rollback.Origin{
		Source:      models.ApplicationEventSourceManual,
		ActorUserID: actorUserID,
		ActorName:   actorName,
	})
	if err != nil {
		if errors.Is(err, application_deployer.ErrAgentOffline) {
			c.JSON(http.StatusConflict, gin.H{"error": "agent is offline; cannot roll back application"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to trigger rollback"})
		return
	}

	utils.RecordAuditLog(c, "rolled-back", "application", id)
	c.JSON(http.StatusAccepted, gin.H{"message": "rollback started"})
	sse.PublishUpdate(sse.TopicApplications)
}

// ResumeApplicationAutoSyncHandler resumes automatic syncs of a rolled back
// application. It keeps running the deployment it was rolled back to until a
// new commit is deployed.
func ResumeApplicationAutoSyncHandler(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	rowsAffected, err := gorm.G[models.Application](db.DB).
		Where("id = ? AND auto_sync_paused = ?", id, true).
		Update(ctx, "auto_sync_paused", false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if rowsAffected == 0 {
		exists, err := hasRecord[models.Application](c, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			return
		}
		if !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": "application not found"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "auto-sync is not paused"})
		return
	}

	utils.RecordAuditLog(c, "resumed-auto-sync", "application", id)
	c.JSON(http.StatusOK, gin.H{"message": "auto-sync resumed"})
	sse.PublishUpdate(sse.TopicApplications)
}
