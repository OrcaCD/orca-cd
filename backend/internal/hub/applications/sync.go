package applications

import (
	"context"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/applicationevents"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	application_deployer "github.com/OrcaCD/orca-cd/internal/hub/deployer"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/notifications"
	"github.com/OrcaCD/orca-cd/internal/hub/repositories"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// SyncOrigin identifies what triggered a sync so history events can attribute it.
type SyncOrigin struct {
	Source      models.ApplicationEventSource
	ActorUserID *string
	ActorName   *string
}

type syncJob struct {
	Application        models.Application
	Repository         models.Repository
	RepositoryProvider repositories.Provider
	Commit             string
	CommitMessage      string
	Origin             SyncOrigin
}

func GetMatchingApplications(ctx context.Context, repository *models.Repository, branch string) ([]models.Application, error) {
	return gorm.G[models.Application](db.DB).Where("repository_id = ? AND branch = ?", repository.Id, branch).Find(ctx)
}

func GetAllApplicationsForRepo(ctx context.Context, repository *models.Repository) ([]models.Application, error) {
	return gorm.G[models.Application](db.DB).Where("repository_id = ?", repository.Id).Find(ctx)
}

func processSyncJob(ctx context.Context, job syncJob, log *zerolog.Logger) {
	// Scheduled polls that resolve to the already-recorded commit are pure no-ops:
	// skip them entirely so the history is not flooded every polling interval.
	// Explicit triggers (manual, webhook, CI) always leave a history record.
	if job.Origin.Source == models.ApplicationEventSourceRepositoryPolling && job.Commit == job.Application.Commit {
		return
	}

	requestID := uuid.NewString()
	if _, err := applicationevents.Start(ctx, syncEventParams(job, &requestID)); err != nil {
		log.Error().Err(err).Str("applicationId", job.Application.Id).Msg("failed to record commit sync event")
	}

	content, err := job.RepositoryProvider.GetFileContent(ctx, &job.Repository, job.Commit, job.Application.Path)
	if err != nil {
		log.Error().Err(err).Str("applicationId", job.Application.Id).
			Msg("failed to fetch compose file during sync")
		failSyncJob(job.Application, log)
		completeSyncEvent(ctx, requestID, job.Application.Id, models.ApplicationEventFailed, "failed to fetch compose file: "+err.Error(), log)
		return
	}

	if content == job.Application.ComposeFile.String() {
		// Compose file unchanged: record the new commit and mark synced without redeploying.
		now := time.Now()
		// Non-nil pointer to "" clears any previous error (GORM skips only nil pointers).
		cleared := ""
		_ = updateApplicationStatus(context.Background(), job.Application.Id, models.Application{
			SyncStatus:    models.Synced,
			Commit:        job.Commit,
			CommitMessage: job.CommitMessage,
			LastSyncedAt:  &now,
			LastSyncError: &cleared,
		}, log)
		completeSyncEvent(ctx, requestID, job.Application.Id, models.ApplicationEventNoChange, "", log)
		return
	}

	deployer := application_deployer.DefaultApplicationDeployer
	if deployer == nil {
		log.Error().Str("applicationId", job.Application.Id).Msg("application deployer not initialized")
		failSyncJob(job.Application, log)
		completeSyncEvent(ctx, requestID, job.Application.Id, models.ApplicationEventFailed, "application deployer not initialized", log)
		return
	}

	if _, err := gorm.G[models.Application](db.DB).
		Where("id = ?", job.Application.Id).
		Select("ComposeFile", "PreviousComposeFile", "Commit", "CommitMessage").
		Updates(context.Background(), models.Application{
			ComposeFile:         crypto.EncryptedString(content),
			PreviousComposeFile: job.Application.ComposeFile,
			Commit:              job.Commit,
			CommitMessage:       job.CommitMessage,
		}); err != nil {
		log.Error().Err(err).Str("applicationId", job.Application.Id).
			Msg("failed to persist compose file before deploy")
		failSyncJob(job.Application, log)
		completeSyncEvent(ctx, requestID, job.Application.Id, models.ApplicationEventFailed, "failed to persist compose file: "+err.Error(), log)
		return
	}

	// The deployer completes the event on dispatch failure; the Agent's deploy
	// result completes it otherwise, so no terminal write happens here.
	if err := deployer.TriggerApplicationDeploy(context.Background(), &job.Application, content, requestID); err != nil {
		log.Error().Err(err).Str("applicationId", job.Application.Id).Msg("failed to trigger application deploy")
		failSyncJob(job.Application, log)
	}
}

func syncEventParams(job syncJob, requestID *string) applicationevents.Params {
	params := applicationevents.Params{
		ApplicationID: job.Application.Id,
		RequestID:     requestID,
		Type:          models.ApplicationEventCommitSync,
		Source:        job.Origin.Source,
		ActorUserID:   job.Origin.ActorUserID,
		ActorName:     job.Origin.ActorName,
	}
	if job.Commit != "" {
		commit := job.Commit
		params.CommitHash = &commit
	}
	if job.CommitMessage != "" {
		message := job.CommitMessage
		params.CommitMessage = &message
	}
	return params
}

func completeSyncEvent(ctx context.Context, requestID, applicationID string, status models.ApplicationEventStatus, errorMessage string, log *zerolog.Logger) {
	var errPtr *string
	if errorMessage != "" {
		errPtr = &errorMessage
	}
	if _, err := applicationevents.Complete(ctx, requestID, applicationID, status, errPtr); err != nil {
		log.Error().Err(err).Str("applicationId", applicationID).Msg("failed to complete commit sync event")
	}
}

// recordSyncFailure writes a terminal failed history event for an application
// whose sync failed before its job could run (resolver, queue, or dispatch setup).
func recordSyncFailure(ctx context.Context, application *models.Application, origin SyncOrigin, commit, commitMessage, errorMessage string, log *zerolog.Logger) {
	// Mirror the processSyncJob skip: a scheduled poll for the already-recorded
	// commit would have been dropped silently, so its queue failure is not recorded.
	if origin.Source == models.ApplicationEventSourceRepositoryPolling && commit != "" && commit == application.Commit {
		return
	}
	params := applicationevents.Params{
		ApplicationID: application.Id,
		Type:          models.ApplicationEventCommitSync,
		Source:        origin.Source,
		ActorUserID:   origin.ActorUserID,
		ActorName:     origin.ActorName,
	}
	if commit != "" {
		params.CommitHash = &commit
	}
	if commitMessage != "" {
		params.CommitMessage = &commitMessage
	}
	if _, err := applicationevents.RecordTerminal(ctx, params, models.ApplicationEventFailed, &errorMessage); err != nil {
		log.Error().Err(err).Str("applicationId", application.Id).Msg("failed to record sync failure event")
	}
}

func updateApplicationStatus(ctx context.Context, applicationID string, updates models.Application, log *zerolog.Logger) error {
	_, err := gorm.G[models.Application](db.DB).
		Where("id = ?", applicationID).
		Updates(ctx, updates)
	if err != nil {
		log.Error().Err(err).Str("applicationId", applicationID).Msg("failed to update application status")
	}
	sse.PublishUpdate("/api/v1/applications")
	return err
}

func failSyncJob(application models.Application, log *zerolog.Logger) {
	_ = updateApplicationStatus(context.Background(), application.Id, models.Application{
		SyncStatus:   models.OutOfSync,
		HealthStatus: models.Unhealthy,
	}, log)
	notifications.SendNotification(application.Id, "Error: sync failed for "+application.Name.String(), log)
}
