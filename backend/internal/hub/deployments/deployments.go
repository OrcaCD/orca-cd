// Package deployments records what applications run, so they can be rolled
// back to an earlier deployment.
package deployments

import (
	"context"
	"errors"
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
	if err := Supersede(ctx, app.Id); err != nil {
		return nil, err
	}
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
		previous, err := lastGood(ctx, tx, app.Id)
		if err != nil {
			return err
		}
		deployment.PreviousDeploymentId = previous
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
// agent with the reported images. With a health gate the deployment is
// verified until the gate ends, otherwise it succeeds right away. It returns
// nil when no deployment was recorded for the request.
func Succeed(ctx context.Context, applicationID, requestID string, images []*messages.ServiceImage, gate time.Duration) (*models.ApplicationDeployment, error) {
	if requestID == "" {
		return nil, nil
	}
	var deployment *models.ApplicationDeployment
	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Writing first makes SQLite lock the database for writing right away;
		// upgrading a read lock fails without waiting for other writers.
		var result models.ApplicationDeployment
		applyResult(&result, images, gate)
		rowsAffected, err := gorm.G[models.ApplicationDeployment](tx).
			Where("application_id = ? AND request_id = ? AND status = ?", applicationID, requestID, models.DeploymentPending).
			Select("Status", "Images", "DeployedAt", "VerifyUntil").
			Updates(ctx, result)
		if err != nil || rowsAffected == 0 {
			return err
		}
		succeeded, err := gorm.G[models.ApplicationDeployment](tx).
			Where("application_id = ? AND request_id = ?", applicationID, requestID).
			First(ctx)
		if err != nil {
			return err
		}
		deployment = &succeeded
		return setCurrent(ctx, tx, &succeeded)
	})
	if err != nil {
		return nil, err
	}
	if deployment != nil {
		publishUpdate(applicationID)
	}
	return deployment, nil
}

// RecordImageUpdate records the images an image update deployed. The update
// ran the application's current compose file at its current commit.
func RecordImageUpdate(ctx context.Context, applicationID, requestID string, images []*messages.ServiceImage, gate time.Duration) (*models.ApplicationDeployment, error) {
	if err := Supersede(ctx, applicationID); err != nil {
		return nil, err
	}
	app, err := gorm.G[models.Application](db.DB).
		Select("id", "commit", "commit_message", "compose_file").
		Where("id = ?", applicationID).
		First(ctx)
	if err != nil {
		return nil, err
	}
	previous, err := lastGood(ctx, db.DB, applicationID)
	if err != nil {
		return nil, err
	}

	deployment := models.ApplicationDeployment{
		ApplicationId:        applicationID,
		Commit:               app.Commit,
		CommitMessage:        app.CommitMessage,
		ComposeFile:          app.ComposeFile,
		PreviousDeploymentId: previous,
	}
	applyResult(&deployment, images, gate)
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
		return setCurrent(ctx, tx, &deployment)
	})
	if err != nil {
		return nil, err
	}
	publishUpdate(applicationID)
	return &deployment, nil
}

func applyResult(deployment *models.ApplicationDeployment, images []*messages.ServiceImage, gate time.Duration) {
	now := time.Now()
	deployment.Images = toDeployedImages(images)
	deployment.DeployedAt = &now
	deployment.Status = models.DeploymentSucceeded
	if gate > 0 {
		verifyUntil := now.Add(gate)
		deployment.Status = models.DeploymentVerifying
		deployment.VerifyUntil = &verifyUntil
	}
}

// Verify marks a deployment whose health gate passed as succeeded and
// completes the event that deployed it. It reports false when the deployment
// is no longer being verified.
func Verify(ctx context.Context, deployment *models.ApplicationDeployment) (bool, error) {
	verified := false
	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rowsAffected, err := gorm.G[models.ApplicationDeployment](tx).
			Where("id = ? AND status = ?", deployment.Id, models.DeploymentVerifying).
			Update(ctx, "status", models.DeploymentSucceeded)
		if err != nil || rowsAffected == 0 {
			return err
		}
		verified = true
		if err := completeEvent(ctx, tx, deployment, models.ApplicationEventSucceeded, nil); err != nil {
			return err
		}
		return prune(ctx, tx, deployment.ApplicationId)
	})
	if verified {
		publishUpdate(deployment.ApplicationId)
	}
	return verified, err
}

// Reject drops a deployment that failed its health gate, which can't be
// rolled back to, and fails the event that deployed it with reason. It
// reports false when the deployment is no longer being verified.
func Reject(ctx context.Context, deployment *models.ApplicationDeployment, reason string) (bool, error) {
	var rejected bool
	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// The event is completed first: deleting the deployment unlinks it. Both
		// only apply while the deployment is verified, so Verify can't interleave.
		if err := completeEvent(ctx, tx, deployment, models.ApplicationEventFailed, &reason,
			"deployment_id IN (SELECT id FROM application_deployments WHERE id = ? AND status = ?)", deployment.Id, models.DeploymentVerifying); err != nil {
			return err
		}
		deleted, err := deleteDeploymentsWhere(ctx, tx, "id = ? AND status = ?", deployment.Id, models.DeploymentVerifying)
		rejected = deleted > 0
		return err
	})
	if rejected {
		publishUpdate(deployment.ApplicationId)
	}
	return rejected, err
}

// completeEvent completes the running event that deployed the deployment.
// Rollback events that target the deployment are not affected.
func completeEvent(ctx context.Context, tx *gorm.DB, deployment *models.ApplicationDeployment, status models.ApplicationEventStatus, errorMessage *string, conds ...any) error {
	now := time.Now()
	q := gorm.G[models.ApplicationEvent](tx).Where(
		"deployment_id = ? AND application_id = ? AND status = ? AND type <> ?",
		deployment.Id, deployment.ApplicationId, models.ApplicationEventRunning, models.ApplicationEventRollback,
	)
	if len(conds) > 0 {
		q = q.Where(conds[0], conds[1:]...)
	}
	_, err := q.Select("Status", "ErrorMessage", "CompletedAt").
		Updates(ctx, models.ApplicationEvent{Status: status, ErrorMessage: errorMessage, CompletedAt: &now})
	return err
}

// supersededError is recorded for a deployment that was replaced before it
// became healthy.
const supersededError = "replaced by a newer deployment before its health was verified"

// Supersede ends the health gate of the application's deployment before it is
// replaced. It counts as succeeded when the application is healthy so far,
// and is dropped otherwise, without rolling back.
func Supersede(ctx context.Context, applicationID string) error {
	verifying, err := Verifying(ctx, applicationID)
	if err != nil || verifying == nil {
		return err
	}
	app, err := gorm.G[models.Application](db.DB).Select("id", "health_status").Where("id = ?", applicationID).First(ctx)
	if err != nil {
		return err
	}

	if app.HealthStatus == models.Healthy {
		_, err = Verify(ctx, verifying)
		return err
	}
	_, err = Reject(ctx, verifying, supersededError)
	return err
}

// RollbackTarget returns the deployment an automatic rollback of the
// application restores, or nil when there is none.
func RollbackTarget(ctx context.Context, app *models.Application) (*models.ApplicationDeployment, error) {
	var target *string
	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := gorm.G[models.Application](tx).Select("id", "current_deployment_id").Where("id = ?", app.Id).First(ctx)
		if err != nil {
			return err
		}
		if current.CurrentDeploymentId != nil {
			verifying, err := gorm.G[models.ApplicationDeployment](tx).
				Select("id", "previous_deployment_id").
				Where("id = ? AND status = ?", *current.CurrentDeploymentId, models.DeploymentVerifying).
				Find(ctx)
			if err != nil {
				return err
			}
			if len(verifying) > 0 {
				target = verifying[0].PreviousDeploymentId
				return nil
			}
		}
		target, err = lastGood(ctx, tx, app.Id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return Get(ctx, app.Id, target)
}

// Verifying returns the deployment of the application whose health gate is
// running, or nil.
func Verifying(ctx context.Context, applicationID string) (*models.ApplicationDeployment, error) {
	deployment, err := gorm.G[models.ApplicationDeployment](db.DB).
		Where("application_id = ? AND status = ?", applicationID, models.DeploymentVerifying).
		First(ctx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &deployment, nil
}

// AllVerifying returns every deployment whose health gate is running.
func AllVerifying(ctx context.Context) ([]models.ApplicationDeployment, error) {
	return gorm.G[models.ApplicationDeployment](db.DB).
		Where("status = ?", models.DeploymentVerifying).
		Find(ctx)
}

// Get returns a succeeded deployment of the application, or nil.
func Get(ctx context.Context, applicationID string, deploymentID *string) (*models.ApplicationDeployment, error) {
	if deploymentID == nil {
		return nil, nil
	}
	deployment, err := gorm.G[models.ApplicationDeployment](db.DB).
		Where("id = ? AND application_id = ? AND status = ?", *deploymentID, applicationID, models.DeploymentSucceeded).
		First(ctx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &deployment, nil
}

// Discard drops the pending deployment of a request that failed and returns
// it, or nil when none was recorded.
func Discard(ctx context.Context, applicationID, requestID string) (*models.ApplicationDeployment, error) {
	if requestID == "" {
		return nil, nil
	}
	pending, err := gorm.G[models.ApplicationDeployment](db.DB).
		Where("application_id = ? AND request_id = ? AND status = ?", applicationID, requestID, models.DeploymentPending).
		First(ctx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var deleted int
	err = db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		deleted, err = deleteDeploymentsWhere(ctx, tx, "id = ? AND status = ?", pending.Id, models.DeploymentPending)
		return err
	})
	if err != nil || deleted == 0 {
		return nil, err
	}
	return &pending, nil
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

// ImageRetention returns which old images the agent keeps for a rollback and
// which images of earlier deployments it removes. It is nil unless the
// application deletes old images and rolls back automatically.
func ImageRetention(ctx context.Context, app *models.Application) (*messages.ImageRetention, error) {
	if !app.RollbackEnabled || !app.DeleteOldImages {
		return nil, nil
	}
	current, err := gorm.G[models.Application](db.DB).Select("id", "current_deployment_id").Where("id = ?", app.Id).First(ctx)
	if err != nil {
		return nil, err
	}
	recorded, err := gorm.G[models.ApplicationDeployment](db.DB).
		Select("id", "status", "images", "previous_deployment_id").
		Where("application_id = ? AND status <> ?", app.Id, models.DeploymentPending).
		Order("deployed_at DESC, id DESC").
		Find(ctx)
	if err != nil {
		return nil, err
	}

	// Keep the images a rollback restores: of the current deployment, and while
	// it is verified, of the deployment before it.
	restored := map[string]bool{}
	for i := range recorded {
		if current.CurrentDeploymentId == nil || recorded[i].Id != *current.CurrentDeploymentId {
			continue
		}
		restored[recorded[i].Id] = true
		if recorded[i].Status == models.DeploymentVerifying && recorded[i].PreviousDeploymentId != nil {
			restored[*recorded[i].PreviousDeploymentId] = true
		}
	}
	if len(restored) == 0 && len(recorded) > 0 {
		restored[recorded[0].Id] = true
	}

	retention := &messages.ImageRetention{}
	keep := map[string]bool{}
	for i := range recorded {
		if !restored[recorded[i].Id] {
			continue
		}
		for _, image := range recorded[i].Images {
			if !keep[image.ImageId] {
				keep[image.ImageId] = true
				retention.KeepImageIds = append(retention.KeepImageIds, image.ImageId)
			}
		}
	}
	removed := map[string]bool{}
	for i := range recorded {
		for _, image := range recorded[i].Images {
			if !keep[image.ImageId] && !removed[image.ImageId] {
				removed[image.ImageId] = true
				retention.RemoveImageIds = append(retention.RemoveImageIds, image.ImageId)
			}
		}
	}
	return retention, nil
}

// lastGood returns the deployment an automatic rollback restores: the one the
// application runs, or the latest succeeded one when that is unknown.
func lastGood(ctx context.Context, tx *gorm.DB, applicationID string) (*string, error) {
	app, err := gorm.G[models.Application](tx).Select("id", "current_deployment_id").Where("id = ?", applicationID).First(ctx)
	if err != nil {
		return nil, err
	}
	succeeded := gorm.G[models.ApplicationDeployment](tx).
		Select("id").
		Where("application_id = ? AND status = ?", applicationID, models.DeploymentSucceeded)
	if app.CurrentDeploymentId != nil {
		current, err := succeeded.Where("id = ?", *app.CurrentDeploymentId).Find(ctx)
		if err != nil || len(current) > 0 {
			return app.CurrentDeploymentId, err
		}
	}
	latest, err := gorm.G[models.ApplicationDeployment](tx).
		Select("id").
		Where("application_id = ? AND status = ?", applicationID, models.DeploymentSucceeded).
		Order("deployed_at DESC, id DESC").
		Limit(1).
		Find(ctx)
	if err != nil || len(latest) == 0 {
		return nil, err
	}
	return &latest[0].Id, nil
}

func deletePending(ctx context.Context, tx *gorm.DB, query string, args ...any) error {
	cond := "status = ?"
	condArgs := []any{models.DeploymentPending}
	if query != "" {
		cond += " AND (" + query + ")"
		condArgs = append(condArgs, args...)
	}
	_, err := deleteDeploymentsWhere(ctx, tx, cond, condArgs...)
	return err
}

// setCurrent makes the deployment the application's current one and, once it
// succeeded, prunes older deployments.
func setCurrent(ctx context.Context, tx *gorm.DB, deployment *models.ApplicationDeployment) error {
	columns := []any{}
	if deployment.Status == models.DeploymentVerifying {
		// The health gate decides on reports about the new containers. The agent
		// sends one after every deployment and image update, after this result.
		columns = append(columns, "HealthStatus")
	}
	if _, err := gorm.G[models.Application](tx).
		Where("id = ?", deployment.ApplicationId).
		Select("CurrentDeploymentId", columns...).
		Updates(ctx, models.Application{CurrentDeploymentId: &deployment.Id, HealthStatus: models.UnknownHealth}); err != nil {
		return err
	}
	if deployment.Status != models.DeploymentSucceeded {
		return nil
	}
	return prune(ctx, tx, deployment.ApplicationId)
}

// prune deletes succeeded deployments beyond MaxPerApplication. It runs when
// the newest deployment succeeds, so it never deletes the current one.
func prune(ctx context.Context, tx *gorm.DB, applicationID string) error {
	succeeded, err := gorm.G[models.ApplicationDeployment](tx).
		Select("id").
		Where("application_id = ? AND status = ?", applicationID, models.DeploymentSucceeded).
		Order("deployed_at DESC, id DESC").
		Find(ctx)
	if err != nil || len(succeeded) <= MaxPerApplication {
		return err
	}
	_, err = deleteDeploymentsWhere(ctx, tx, "id IN ?", deploymentIDs(succeeded[MaxPerApplication:]))
	return err
}

// deleteDeploymentsWhere deletes the deployments matching cond and clears the
// references to them, which have no foreign key (see migration 000037). Every
// statement writes, so a transaction that starts with it locks the database
// for writing right away. It returns the number of deleted deployments.
func deleteDeploymentsWhere(ctx context.Context, tx *gorm.DB, cond string, args ...any) (int, error) {
	matching := "IN (SELECT id FROM application_deployments WHERE " + cond + ")"
	if _, err := gorm.G[models.ApplicationEvent](tx).
		Where("deployment_id "+matching, args...).
		Update(ctx, "deployment_id", nil); err != nil {
		return 0, err
	}
	if _, err := gorm.G[models.Application](tx).
		Where("current_deployment_id "+matching, args...).
		Update(ctx, "current_deployment_id", nil); err != nil {
		return 0, err
	}
	if _, err := gorm.G[models.ApplicationDeployment](tx).
		Where("previous_deployment_id "+matching, args...).
		Update(ctx, "previous_deployment_id", nil); err != nil {
		return 0, err
	}
	return gorm.G[models.ApplicationDeployment](tx).Where(cond, args...).Delete(ctx)
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
	sse.PublishUpdate(sse.TopicApplications, sse.TopicApplicationEvents(applicationID), sse.TopicApplicationActivity)
}
