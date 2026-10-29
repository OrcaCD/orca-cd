package websocket

import (
	"context"
	"fmt"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/applicationevents"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/deployments"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/notifications"
	"github.com/OrcaCD/orca-cd/internal/hub/rollback"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// healthGateInterval is how often ended health gates are resolved.
const healthGateInterval = 10 * time.Second

// A deployment of an application that rolls back when it becomes unhealthy is
// watched for the application's health timeout (its health gate). Becoming
// unhealthy, stopped or degraded during the gate fails it right away; not
// being healthy when the gate ends fails it too. A failed deployment is
// rolled back to the deployment before it.

// StartHealthGate resolves ended health gates in the background until the
// returned stop function is called.
func StartHealthGate(log *zerolog.Logger) (stop func()) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(healthGateInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				resolveEndedHealthGates(ctx, now, log)
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

func resolveEndedHealthGates(parent context.Context, now time.Time, log *zerolog.Logger) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()

	due, err := deployments.VerificationsDue(ctx, now)
	if err != nil {
		log.Error().Err(err).Msg("failed to load deployments with an ended health check")
		return
	}
	for i := range due {
		app, err := gorm.G[models.Application](db.DB).Where("id = ?", due[i].ApplicationId).First(ctx)
		if err != nil {
			log.Error().Err(err).Str("applicationId", due[i].ApplicationId).Msg("failed to load application for health check")
			continue
		}
		// Health is unknown while the agent is offline, which says nothing about
		// the deployment. The gate is resolved once the agent reports again.
		if DefaultHub == nil || !DefaultHub.IsRegistered(app.AgentId) {
			continue
		}
		if app.HealthStatus == models.Healthy {
			passHealthGate(ctx, &app, &due[i], log)
			continue
		}
		reason := fmt.Sprintf("health check failed: application did not become healthy within %s", formatSeconds(gateDuration(&due[i])))
		failHealthGate(ctx, &app, &due[i], reason, log)
	}
}

// checkHealthGate fails the health gate of the application's deployment when
// the agent reports it as failing.
func checkHealthGate(ctx context.Context, agentID, applicationID string, health models.HealthStatus, log *zerolog.Logger) {
	if !isFailingHealth(health) {
		return
	}
	deployment, err := deployments.Verifying(ctx, applicationID)
	if err != nil {
		log.Error().Err(err).Str("applicationId", applicationID).Msg("failed to load deployment for health check")
		return
	}
	if deployment == nil {
		return
	}
	app, err := getApplicationByID(ctx, agentID, applicationID, log)
	if err != nil {
		return
	}
	elapsed := time.Duration(0)
	if deployment.DeployedAt != nil {
		elapsed = time.Since(*deployment.DeployedAt)
	}
	reason := fmt.Sprintf("health check failed: application was %s %s after the deployment", health, formatSeconds(elapsed))
	failHealthGate(ctx, app, deployment, reason, log)
}

func passHealthGate(ctx context.Context, app *models.Application, deployment *models.ApplicationDeployment, log *zerolog.Logger) {
	verified, err := deployments.Verify(ctx, deployment)
	if err != nil {
		log.Error().Err(err).Str("applicationId", app.Id).Msg("failed to mark deployment as verified")
		return
	}
	if !verified {
		return
	}
	eventType := gatedEventType(ctx, app.Id, deployment, log)
	if _, err := applicationevents.CompleteForDeployment(ctx, app.Id, deployment.Id, models.ApplicationEventSucceeded, nil); err != nil {
		log.Error().Err(err).Str("applicationId", app.Id).Msg("failed to complete verified deployment event")
	}
	sendRetentionSettings(ctx, app, log)

	if eventType == models.ApplicationEventImageUpdate {
		go notifications.SendForApplication(app.Id, models.NotificationEventImageUpdateSucceeded, "Success: image update succeeded for "+app.Name.String(), log)
		return
	}
	go notifications.SendForApplication(app.Id, models.NotificationEventDeploymentSucceeded, "Success: deployment succeeded for "+app.Name.String(), log)
}

func failHealthGate(ctx context.Context, app *models.Application, deployment *models.ApplicationDeployment, reason string, log *zerolog.Logger) {
	eventType := gatedEventType(ctx, app.Id, deployment, log)
	// Completed before the deployment is dropped, which unlinks its event.
	if _, err := applicationevents.CompleteForDeployment(ctx, app.Id, deployment.Id, models.ApplicationEventFailed, &reason); err != nil {
		log.Error().Err(err).Str("applicationId", app.Id).Msg("failed to complete unhealthy deployment event")
	}
	rejected, err := deployments.Reject(ctx, deployment)
	if err != nil {
		log.Error().Err(err).Str("applicationId", app.Id).Msg("failed to drop unhealthy deployment")
		return
	}
	if !rejected {
		return
	}

	if _, err := gorm.G[models.Application](db.DB).
		Where("id = ?", app.Id).
		Updates(ctx, models.Application{SyncStatus: models.OutOfSync, LastSyncError: &reason}); err != nil {
		log.Error().Err(err).Str("applicationId", app.Id).Msg("failed to update application after failed health check")
	}
	sse.PublishUpdate(sse.TopicApplications)

	event, message := models.NotificationEventDeploymentFailed, "Error: deployment failed for "+app.Name.String()+": "+reason
	if eventType == models.ApplicationEventImageUpdate {
		event, message = models.NotificationEventImageUpdateFailed, "Error: image update failed for "+app.Name.String()+": "+reason
	}
	go notifications.SendForApplication(app.Id, event, message, log)

	target, err := deployments.Get(ctx, app.Id, deployment.PreviousDeploymentId)
	if err != nil {
		log.Error().Err(err).Str("applicationId", app.Id).Msg("failed to load deployment to roll back to")
		return
	}
	triggerAutomaticRollback(ctx, app, target, models.ApplicationEventSourceHealthCheck, log)
}

// triggerAutomaticRollback rolls the application back to target after a
// failed deployment, if there is a deployment to roll back to.
func triggerAutomaticRollback(ctx context.Context, app *models.Application, target *models.ApplicationDeployment, source models.ApplicationEventSource, log *zerolog.Logger) {
	if target == nil {
		log.Warn().Str("applicationId", app.Id).Msg("no earlier deployment to roll back to")
		return
	}
	log.Info().Str("applicationId", app.Id).Str("deploymentId", target.Id).Str("source", string(source)).Msg("rolling back application")
	if err := rollback.Trigger(ctx, app, target, rollback.Origin{Source: source}); err != nil {
		log.Error().Err(err).Str("applicationId", app.Id).Msg("failed to trigger automatic rollback")
		go notifications.SendForApplication(app.Id, models.NotificationEventRollbackFailed, "Error: rollback failed for "+app.Name.String()+": "+err.Error(), log)
	}
}

// automaticRollbackTarget returns the deployment to roll back to after a
// failed deployment, or nil when the application doesn't roll back on failure.
func automaticRollbackTarget(ctx context.Context, app *models.Application, previous *string, log *zerolog.Logger) *models.ApplicationDeployment {
	if !app.RollbackEnabled || !app.RollbackOnDeployFailure {
		return nil
	}
	target, err := deployments.Get(ctx, app.Id, previous)
	if err == nil && target == nil {
		target, err = deployments.RollbackTarget(ctx, app)
	}
	if err != nil {
		log.Error().Err(err).Str("applicationId", app.Id).Msg("failed to load deployment to roll back to")
		return nil
	}
	if target == nil {
		log.Warn().Str("applicationId", app.Id).Msg("no earlier deployment to roll back to")
	}
	return target
}

// sendRetentionSettings pushes the agent settings again when the images kept
// for rollbacks changed.
func sendRetentionSettings(ctx context.Context, app *models.Application, log *zerolog.Logger) {
	if app.RollbackEnabled && app.DeleteOldImages {
		sendAgentSettings(ctx, app.AgentId, log)
	}
}

func gatedEventType(ctx context.Context, applicationID string, deployment *models.ApplicationDeployment, log *zerolog.Logger) models.ApplicationEventType {
	if deployment.RequestId == nil {
		return models.ApplicationEventDeployment
	}
	event, err := applicationevents.ForRequest(ctx, *deployment.RequestId, applicationID)
	if err != nil {
		log.Error().Err(err).Str("applicationId", applicationID).Msg("failed to load event of verified deployment")
	}
	if event == nil {
		return models.ApplicationEventDeployment
	}
	return event.Type
}

func isFailingHealth(health models.HealthStatus) bool {
	return health == models.Unhealthy || health == models.Stopped || health == models.Degraded
}

func gateDuration(deployment *models.ApplicationDeployment) time.Duration {
	if deployment.DeployedAt == nil || deployment.VerifyUntil == nil {
		return 0
	}
	return deployment.VerifyUntil.Sub(*deployment.DeployedAt)
}

func formatSeconds(d time.Duration) string {
	return fmt.Sprintf("%ds", int64(d.Round(time.Second)/time.Second))
}
