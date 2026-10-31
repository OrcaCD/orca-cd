// Package deployments records what applications run, so they can be rolled
// back to an earlier deployment.
package deployments

import (
	"context"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"gorm.io/gorm"
)

// MaxPerApplication is the number of succeeded deployments kept per
// application as rollback targets.
const MaxPerApplication = 10

// Start records a deployment of composeFile at the application's commit that
// is about to be dispatched with requestID. Pending deployments of earlier
// requests are dropped: their result is either lost or superseded.
func Start(ctx context.Context, app *models.Application, composeFile, requestID string) (*models.ApplicationDeployment, error) {
	deployment := models.ApplicationDeployment{
		ApplicationId: app.Id,
		RequestId:     &requestID,
		Status:        models.DeploymentPending,
		Commit:        app.Commit,
		CommitMessage: app.CommitMessage,
		ComposeFile:   crypto.EncryptedString(composeFile),
		Images:        []models.DeployedImage{},
	}
	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := deletePending(ctx, tx, "application_id = ?", app.Id); err != nil {
			return err
		}
		if err := gorm.G[models.ApplicationDeployment](tx).Create(ctx, &deployment); err != nil {
			return err
		}
		return linkEvent(ctx, tx, app.Id, requestID, deployment.Id)
	})
	if err != nil {
		return nil, err
	}
	return &deployment, nil
}

// Succeed marks the deployment dispatched with requestID as running on the
// agent with the reported images. It reports false when no deployment was
// recorded for the request.
func Succeed(ctx context.Context, applicationID, requestID string, images []*messages.ServiceImage) (bool, error) {
	if requestID == "" {
		return false, nil
	}
	found := false
	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Writing first makes SQLite lock the database for writing right away;
		// upgrading a read lock fails without waiting for other writers.
		now := time.Now()
		rowsAffected, err := gorm.G[models.ApplicationDeployment](tx).
			Where("application_id = ? AND request_id = ? AND status = ?", applicationID, requestID, models.DeploymentPending).
			Select("Status", "Images", "DeployedAt").
			Updates(ctx, models.ApplicationDeployment{
				Status:     models.DeploymentSucceeded,
				Images:     toDeployedImages(images),
				DeployedAt: &now,
			})
		if err != nil || rowsAffected == 0 {
			return err
		}
		found = true
		deployment, err := gorm.G[models.ApplicationDeployment](tx).
			Select("id").
			Where("application_id = ? AND request_id = ?", applicationID, requestID).
			First(ctx)
		if err != nil {
			return err
		}
		return setCurrent(ctx, tx, applicationID, deployment.Id)
	})
	if err != nil {
		return false, err
	}
	if found {
		publishUpdate(applicationID)
	}
	return found, nil
}

// RecordImageUpdate records the images an image update deployed. The update
// ran the application's current compose file at its current commit.
func RecordImageUpdate(ctx context.Context, applicationID, requestID string, images []*messages.ServiceImage) (*models.ApplicationDeployment, error) {
	app, err := gorm.G[models.Application](db.DB).
		Select("id", "commit", "commit_message", "compose_file").
		Where("id = ?", applicationID).
		First(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	deployment := models.ApplicationDeployment{
		ApplicationId: applicationID,
		Status:        models.DeploymentSucceeded,
		Commit:        app.Commit,
		CommitMessage: app.CommitMessage,
		ComposeFile:   app.ComposeFile,
		Images:        toDeployedImages(images),
		DeployedAt:    &now,
	}
	if requestID != "" {
		deployment.RequestId = &requestID
	}
	err = db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := gorm.G[models.ApplicationDeployment](tx).Create(ctx, &deployment); err != nil {
			return err
		}
		if requestID != "" {
			if err := linkEvent(ctx, tx, applicationID, requestID, deployment.Id); err != nil {
				return err
			}
		}
		return setCurrent(ctx, tx, applicationID, deployment.Id)
	})
	if err != nil {
		return nil, err
	}
	publishUpdate(applicationID)
	return &deployment, nil
}

// Discard drops the pending deployment of a request that failed.
func Discard(ctx context.Context, applicationID, requestID string) error {
	if requestID == "" {
		return nil
	}
	return db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return deletePending(ctx, tx, "application_id = ? AND request_id = ?", applicationID, requestID)
	})
}

// DiscardPendingForAgent drops the pending deployments of an agent's
// applications. Their results can't arrive anymore once the agent disconnected.
func DiscardPendingForAgent(ctx context.Context, agentID string) error {
	return db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return deletePending(ctx, tx, "application_id IN (SELECT id FROM applications WHERE agent_id = ?)", agentID)
	})
}

// DiscardAllPending drops every pending deployment. Used at startup, when no
// agent is connected that could still report a result.
func DiscardAllPending(ctx context.Context) error {
	return db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return deletePending(ctx, tx, "")
	})
}

func deletePending(ctx context.Context, tx *gorm.DB, query string, args ...any) error {
	cond := "status = ?"
	condArgs := []any{models.DeploymentPending}
	if query != "" {
		cond += " AND (" + query + ")"
		condArgs = append(condArgs, args...)
	}
	return deleteDeploymentsWhere(ctx, tx, cond, condArgs...)
}

// setCurrent makes deploymentID the application's current deployment and
// prunes succeeded deployments beyond MaxPerApplication.
func setCurrent(ctx context.Context, tx *gorm.DB, applicationID, deploymentID string) error {
	if _, err := gorm.G[models.Application](tx).
		Where("id = ?", applicationID).
		Update(ctx, "current_deployment_id", deploymentID); err != nil {
		return err
	}

	succeeded, err := gorm.G[models.ApplicationDeployment](tx).
		Select("id").
		Where("application_id = ? AND status = ?", applicationID, models.DeploymentSucceeded).
		Order("deployed_at DESC, id DESC").
		Find(ctx)
	if err != nil || len(succeeded) <= MaxPerApplication {
		return err
	}
	return deleteDeploymentsWhere(ctx, tx, "id IN ?", deploymentIDs(succeeded[MaxPerApplication:]))
}

// deleteDeploymentsWhere deletes the deployments matching cond and clears the
// references to them, which have no foreign key (see migration 000037). Every
// statement writes, so a transaction that starts with it locks the database
// for writing right away.
func deleteDeploymentsWhere(ctx context.Context, tx *gorm.DB, cond string, args ...any) error {
	matching := "IN (SELECT id FROM application_deployments WHERE " + cond + ")"
	if _, err := gorm.G[models.ApplicationEvent](tx).
		Where("deployment_id "+matching, args...).
		Update(ctx, "deployment_id", nil); err != nil {
		return err
	}
	if _, err := gorm.G[models.Application](tx).
		Where("current_deployment_id "+matching, args...).
		Update(ctx, "current_deployment_id", nil); err != nil {
		return err
	}
	_, err := gorm.G[models.ApplicationDeployment](tx).Where(cond, args...).Delete(ctx)
	return err
}

func linkEvent(ctx context.Context, tx *gorm.DB, applicationID, requestID, deploymentID string) error {
	_, err := gorm.G[models.ApplicationEvent](tx).
		Where("application_id = ? AND request_id = ?", applicationID, requestID).
		Update(ctx, "deployment_id", deploymentID)
	return err
}

func deploymentIDs(deployments []models.ApplicationDeployment) []string {
	ids := make([]string, 0, len(deployments))
	for i := range deployments {
		ids = append(ids, deployments[i].Id)
	}
	return ids
}

func toDeployedImages(images []*messages.ServiceImage) []models.DeployedImage {
	deployed := make([]models.DeployedImage, 0, len(images))
	for _, image := range images {
		if image.GetService() == "" || image.GetImageId() == "" {
			continue
		}
		deployed = append(deployed, models.DeployedImage{
			Service:   image.GetService(),
			ImageId:   image.GetImageId(),
			PinnedRef: image.GetPinnedRef(),
		})
	}
	return deployed
}

// ToServiceImages converts deployed images into the pins of a deploy request.
func ToServiceImages(images []models.DeployedImage) []*messages.ServiceImage {
	pins := make([]*messages.ServiceImage, 0, len(images))
	for _, image := range images {
		pins = append(pins, &messages.ServiceImage{
			Service:   image.Service,
			ImageId:   image.ImageId,
			PinnedRef: image.PinnedRef,
		})
	}
	return pins
}

func publishUpdate(applicationID string) {
	sse.PublishUpdate(sse.TopicApplications, sse.TopicApplicationEvents(applicationID))
}
