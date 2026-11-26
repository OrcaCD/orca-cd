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
	"gorm.io/gorm/clause"
)

// SyncOrigin identifies what triggered a sync so history events can attribute it.
type SyncOrigin struct {
	Source      models.ApplicationEventSource
	ActorUserID *string
	ActorName   *string
	TriggerID   *string
	// WebhookImages limit an image pull to these images, e.g. the ones an
	// image pull webhook announced.
	WebhookImages []string
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

const settingsChangedDuringSync = "application changed during sync; sync again to deploy the current settings"

func processSyncJob(ctx context.Context, job syncJob, log *zerolog.Logger) {
	// Scheduled polls are edge-triggered by commit. Reprocessing an already-recorded
	// commit could incorrectly clear a prior deployment failure without redeploying.
	if job.Origin.Source == models.ApplicationEventSourceRepositoryPolling && job.Commit == job.Application.Commit && job.Application.PendingFilesRevision == "" {
		return
	}
	if skipAutoSyncAfterRollback(job) {
		log.Info().Str("applicationId", job.Application.Id).Str("commit", job.Commit).
			Msg("skipping automatic sync of rolled back application")
		recordTriggerSkip(ctx, job.Origin.TriggerID, job.Application.Id, models.ApplicationEventCommitSync, "automatic sync was skipped after rollback", log)
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

	files, err := FetchAdditionalFiles(ctx, job.RepositoryProvider, &job.Repository, job.Commit, job.Application.Path, job.Application.AdditionalFilePaths)
	if err != nil {
		message := err.Error()
		log.Error().Err(err).Str("applicationId", job.Application.Id).Msg("failed to fetch additional files during sync")
		_ = updateApplicationStatus(context.Background(), job.Application.Id, models.Application{LastSyncError: &message}, log)
		failSyncJob(job.Application, log)
		completeSyncEvent(ctx, requestID, job.Application.Id, models.ApplicationEventFailed, message, log)
		return
	}
	// A queued commit must not replace settings saved while its files were
	// being fetched.
	source := gorm.G[models.Application](db.DB).
		Where("id = ? AND repository_id = ? AND branch = ? AND path = ? AND agent_id = ? AND \"commit\" = ? AND pending_files_revision = ?",
			job.Application.Id, job.Application.RepositoryId, job.Application.Branch, job.Application.Path,
			job.Application.AgentId, job.Application.Commit, job.Application.PendingFilesRevision)
	composeChanged := content != job.Application.ComposeFile.String()
	filesChanged := files != job.Application.AdditionalFiles || job.Application.PendingFilesRevision != ""

	// A rolled back application runs an earlier compose file, so a new commit
	// is deployed even when it doesn't change the compose file.
	if !composeChanged && !filesChanged && job.Application.RolledBackAt == nil {
		// Compose file unchanged: record the new commit and mark synced without redeploying.
		now := time.Now()
		// Non-nil pointer to "" clears any previous error (GORM skips only nil pointers).
		cleared := ""
		origin := job.Origin.Source
		rows, err := source.Updates(context.Background(), models.Application{
			SyncStatus:     models.Synced,
			Commit:         job.Commit,
			CommitMessage:  job.CommitMessage,
			LastSyncedAt:   &now,
			LastSyncError:  &cleared,
			LastSyncSource: &origin,
		})
		if err != nil {
			log.Error().Err(err).Str("applicationId", job.Application.Id).Msg("failed to persist application sync status")
			completeSyncEvent(ctx, requestID, job.Application.Id, models.ApplicationEventFailed, "failed to persist application sync status: "+err.Error(), log)
			return
		}
		if rows == 0 {
			completeSyncEvent(ctx, requestID, job.Application.Id, models.ApplicationEventFailed, settingsChangedDuringSync, log)
			return
		}
		sse.PublishUpdate(sse.TopicApplications.For(job.Application.Id))
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

	updates := models.Application{
		ComposeFile:         crypto.EncryptedString(content),
		AdditionalFiles:     files,
		Warnings:            job.Application.Warnings,
		PreviousComposeFile: job.Application.PreviousComposeFile,
		Commit:              job.Commit,
		CommitMessage:       job.CommitMessage,
	}
	if composeChanged {
		updates.Warnings = "[]"
		updates.PreviousComposeFile = job.Application.ComposeFile
	}
	rows, err := source.
		Select("ComposeFile", "AdditionalFiles", "Warnings", "PreviousComposeFile", "Commit", "CommitMessage").
		Updates(context.Background(), updates)
	if err != nil {
		log.Error().Err(err).Str("applicationId", job.Application.Id).
			Msg("failed to persist compose file before deploy")
		failSyncJob(job.Application, log)
		completeSyncEvent(ctx, requestID, job.Application.Id, models.ApplicationEventFailed, "failed to persist compose file: "+err.Error(), log)
		return
	}

	if rows == 0 {
		completeSyncEvent(ctx, requestID, job.Application.Id, models.ApplicationEventFailed, settingsChangedDuringSync, log)
		return
	}

	// The deployment is recorded with the application's new commit.
	job.Application.ComposeFile = updates.ComposeFile
	job.Application.AdditionalFiles = files
	job.Application.Commit = job.Commit
	job.Application.CommitMessage = job.CommitMessage

	// The deployer completes the event on dispatch failure; the Agent's deploy
	// result completes it otherwise, so this path does not write a final status.
	if err := deployer.TriggerApplicationDeploy(context.Background(), &job.Application, content, requestID); err != nil {
		log.Error().Err(err).Str("applicationId", job.Application.Id).Msg("failed to trigger application deploy")
		failSyncJob(job.Application, log)
	}
}

// skipAutoSyncAfterRollback reports whether an automatic sync must not deploy
// a rolled back application: while auto-sync is paused not at all, otherwise
// not the commit it was rolled back from, which would fail again.
func skipAutoSyncAfterRollback(job syncJob) bool {
	app := job.Application
	if app.RolledBackAt == nil || !job.Origin.Source.IsAutomatic() {
		return false
	}
	return app.AutoSyncPaused || job.Commit == app.Commit
}

func syncEventParams(job syncJob, requestID *string) applicationevents.Params {
	params := applicationevents.Params{
		ApplicationID: job.Application.Id,
		RequestID:     requestID,
		TriggerID:     job.Origin.TriggerID,
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
	if requestID == "" {
		return
	}
	var errPtr *string
	if errorMessage != "" {
		errPtr = &errorMessage
	}
	eventCtx, cancel := applicationEventWriteContext(ctx)
	defer cancel()
	if _, err := applicationevents.Complete(eventCtx, requestID, applicationID, status, errPtr); err != nil {
		log.Error().Err(err).Str("applicationId", applicationID).Msg("failed to complete commit sync event")
	}
}

// recordSyncFailure writes a completed failed history event for an application
// whose sync failed before its job could run (resolver, queue, or dispatch setup).
func recordSyncFailure(ctx context.Context, application *models.Application, origin SyncOrigin, commit, commitMessage, errorMessage string, log *zerolog.Logger) {
	// Mirror the processSyncJob skip: a scheduled poll for the already-recorded
	// commit would have been dropped silently, so its queue failure is not recorded.
	if origin.Source == models.ApplicationEventSourceRepositoryPolling && commit != "" && commit == application.Commit && application.PendingFilesRevision == "" {
		return
	}
	params := applicationevents.Params{
		ApplicationID: application.Id,
		TriggerID:     origin.TriggerID,
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
	eventCtx, cancel := applicationEventWriteContext(ctx)
	defer cancel()
	if _, err := applicationevents.RecordCompleted(eventCtx, params, models.ApplicationEventFailed, &errorMessage); err != nil {
		log.Error().Err(err).Str("applicationId", application.Id).Msg("failed to record sync failure event")
	}
}

func recordTriggerSkip(ctx context.Context, triggerID *string, applicationID string, operation models.ApplicationEventType, reason string, log *zerolog.Logger) {
	if triggerID == nil {
		return
	}
	eventCtx, cancel := applicationEventWriteContext(ctx)
	defer cancel()
	skip := models.GitHubActionsTriggerSkip{
		TriggerId: *triggerID, ApplicationId: applicationID, Type: operation, Reason: reason,
	}
	if err := gorm.G[models.GitHubActionsTriggerSkip](db.DB, clause.OnConflict{DoNothing: true}).Create(eventCtx, &skip); err != nil {
		log.Error().Err(err).Str("applicationId", applicationID).Msg("failed to record skipped GitHub Actions operation")
	}
}

func applicationEventWriteContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
}

func updateApplicationStatus(ctx context.Context, applicationID string, updates models.Application, log *zerolog.Logger) error {
	_, err := gorm.G[models.Application](db.DB).
		Where("id = ?", applicationID).
		Updates(ctx, updates)
	if err != nil {
		log.Error().Err(err).Str("applicationId", applicationID).Msg("failed to update application status")
	}
	sse.PublishUpdate(sse.TopicApplications.For(applicationID))
	return err
}

func failSyncJob(application models.Application, log *zerolog.Logger) {
	_ = updateApplicationStatus(context.Background(), application.Id, models.Application{
		SyncStatus: models.OutOfSync,
	}, log)
	notifications.SendForApplication(application.Id, models.NotificationEventSyncFailed, "Error: sync failed for "+application.Name.String(), log)
}
