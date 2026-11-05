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

const (
	// healthGateInterval is how often health gates are checked.
	healthGateInterval = 10 * time.Second
	// healthGateGrace is how long an application has to keep failing before its
	// health gate fails early. It rides out a container that restarts once, e.g.
	// while it waits for its database.
	healthGateGrace = 30 * time.Second
	// healthReportTimeout is how long an ended health gate waits for the agent to
	// report the deployment's health, e.g. while it is offline.
	healthReportTimeout = 10 * time.Minute
)

// A deployment of an application that rolls back when it becomes unhealthy is
// watched for the application's health timeout (its health gate). Staying
// unhealthy, stopped or degraded for healthGateGrace fails it early; not being
// healthy when the gate ends fails it too. A failed deployment is rolled back
// to the deployment before it. Only reports the agent sent after the
// deployment count: a gate the agent doesn't report on fails without rolling
// back after healthReportTimeout.
//
// An application counts as unhealthy while any of its containers isn't
// running, so one that runs one-shot containers (such as migrations) fails
// its health gate.

// StartHealthGate checks health gates in the background until the returned
// stop function is called.
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
				resolveHealthGates(ctx, now, log)
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

func resolveHealthGates(parent context.Context, now time.Time, log *zerolog.Logger) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()

	verifying, err := deployments.AllVerifying(ctx)
	if err != nil {
		log.Error().Err(err).Msg("failed to load deployments under a health check")
		return
	}
	for i := range verifying {
		deployment := &verifying[i]
		app, err := gorm.G[models.Application](db.DB).Where("id = ?", deployment.ApplicationId).First(ctx)
		if err != nil {
			log.Error().Err(err).Str("applicationId", deployment.ApplicationId).Msg("failed to load application for health check")
			continue
		}
		resolveHealthGate(ctx, &app, deployment, now, log)
	}
}

func resolveHealthGate(ctx context.Context, app *models.Application, deployment *models.ApplicationDeployment, now time.Time, log *zerolog.Logger) {
	// Turning the health check off ends the running one, without rolling back.
	if app.HealthGate() == 0 {
		passHealthGate(ctx, app, deployment, log)
		return
	}
	report, reported := deploymentHealthReport(app, deployment)
	end := gateEnd(deployment)

	if reported && isFailingHealth(report.health) {
		failingSince := report.failingSince
		if at := deployedAt(deployment); at.After(failingSince) {
			failingSince = at
		}
		if failing := now.Sub(failingSince); failing >= healthGateGrace {
			reason := fmt.Sprintf("health check failed: application was %s for %s after the deployment", report.health, formatSeconds(failing))
			failHealthGate(ctx, app, deployment, reason, true, log)
			return
		}
	}
	switch {
	case now.Before(end):
	case reported && report.health == models.Healthy:
		passHealthGate(ctx, app, deployment, log)
	case reported:
		reason := fmt.Sprintf("health check failed: application did not become healthy within %s", formatSeconds(gateDuration(deployment)))
		failHealthGate(ctx, app, deployment, reason, true, log)
	case !now.Before(end.Add(healthReportTimeout)):
		reason := "health check failed: the agent did not report the application's health"
		failHealthGate(ctx, app, deployment, reason, false, log)
	}
}

// deploymentHealthReport returns the health the application's agent reported
// after the deployment. Earlier reports, of the deployment before it or of an
// earlier connection, say nothing about it.
func deploymentHealthReport(app *models.Application, deployment *models.ApplicationDeployment) (healthReport, bool) {
	if DefaultHub == nil {
		return healthReport{}, false
	}
	report, ok := DefaultHub.lastHealthReport(app.AgentId, app.Id)
	if !ok || report.at.Before(deployedAt(deployment)) {
		return healthReport{}, false
	}
	return report, true
}

func passHealthGate(ctx context.Context, app *models.Application, deployment *models.ApplicationDeployment, log *zerolog.Logger) {
	eventType := gatedEventType(ctx, app.Id, deployment, log)
	verified, err := deployments.Verify(ctx, deployment)
	if err != nil {
		log.Error().Err(err).Str("applicationId", app.Id).Msg("failed to mark deployment as verified")
		return
	}
	if !verified {
		return
	}
	sendRetentionSettings(ctx, app, log)

	if eventType == models.ApplicationEventImageUpdate {
		go notifications.SendForApplication(app.Id, models.NotificationEventImageUpdateSucceeded, "Success: image update succeeded for "+app.Name.String(), log)
		return
	}
	go notifications.SendForApplication(app.Id, models.NotificationEventDeploymentSucceeded, "Success: deployment succeeded for "+app.Name.String(), log)
}

// failHealthGate drops the deployment and, if rollBack is set, rolls back to
// the deployment before it.
func failHealthGate(ctx context.Context, app *models.Application, deployment *models.ApplicationDeployment, reason string, rollBack bool, log *zerolog.Logger) {
	eventType := gatedEventType(ctx, app.Id, deployment, log)
	rejected, err := deployments.Reject(ctx, deployment, reason)
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
	sse.PublishUpdate(sse.TopicApplications.For(app.Id))

	event, message := models.NotificationEventDeploymentFailed, "Error: deployment failed for "+app.Name.String()+": "+reason
	if eventType == models.ApplicationEventImageUpdate {
		event, message = models.NotificationEventImageUpdateFailed, "Error: image update failed for "+app.Name.String()+": "+reason
	}
	go notifications.SendForApplication(app.Id, event, message, log)
	if !rollBack {
		return
	}

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
// for rollbacks changed. Only periodic image updates use the settings' retention.
func sendRetentionSettings(ctx context.Context, app *models.Application, log *zerolog.Logger) {
	if app.RollbackEnabled && app.DeleteOldImages && app.ImagePollEnabled {
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
	return gateEnd(deployment).Sub(deployedAt(deployment))
}

// gateEnd returns when the deployment's health gate ends.
func gateEnd(deployment *models.ApplicationDeployment) time.Time {
	if deployment.VerifyUntil == nil {
		return deployedAt(deployment)
	}
	return *deployment.VerifyUntil
}

func deployedAt(deployment *models.ApplicationDeployment) time.Time {
	if deployment.DeployedAt == nil {
		return time.Time{}
	}
	return *deployment.DeployedAt
}

func formatSeconds(d time.Duration) string {
	return fmt.Sprintf("%ds", int64(d.Round(time.Second)/time.Second))
}
