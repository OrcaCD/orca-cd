package applications

import (
	"context"
	"sync"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/applicationevents"
	"github.com/OrcaCD/orca-cd/internal/hub/composeconfig"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	application_deployer "github.com/OrcaCD/orca-cd/internal/hub/deployer"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/notifications"
	"github.com/OrcaCD/orca-cd/internal/hub/repositories"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	hubws "github.com/OrcaCD/orca-cd/internal/hub/websocket"
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

func processSyncJob(ctx context.Context, job syncJob, log *zerolog.Logger) {
	// Scheduled polls are edge-triggered by commit. Reprocessing an already-recorded
	// commit could incorrectly clear a prior deployment failure without redeploying.
	if job.Origin.Source == models.ApplicationEventSourceRepositoryPolling && job.Commit == job.Application.Commit {
		return
	}
	if skipAutoSyncAfterRollback(job) {
		log.Info().Str("applicationId", job.Application.Id).Str("commit", job.Commit).
			Msg("skipping automatic sync of rolled back application")
		recordTriggerSkip(ctx, job.Origin.TriggerID, job.Application.Id, models.ApplicationEventCommitSync, "automatic sync was skipped after rollback", log)
		return
	}
	if job.Origin.Source == models.ApplicationEventSourceRepositoryPolling && rejectedConfigs.has(job.Application.Id, job.Commit) {
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

	cfg, err := composeconfig.Parse(content)
	if err != nil {
		message := "invalid " + composeconfig.Key + " in the compose file: " + err.Error()
		log.Warn().Err(err).Str("applicationId", job.Application.Id).Msg("invalid compose settings during sync")
		rejectedConfigs.set(job.Application.Id, job.Commit)
		failSyncJobWithError(job.Application, message, log)
		completeSyncEvent(ctx, requestID, job.Application.Id, models.ApplicationEventFailed, message, log)
		return
	}
	rejectedConfigs.set(job.Application.Id, "")

	// The settings are applied to their values now, not when the job was
	// queued, so changes made through the API meanwhile are kept.
	current, err := gorm.G[models.Application](db.DB).Where("id = ?", job.Application.Id).First(ctx)
	if err != nil {
		log.Error().Err(err).Str("applicationId", job.Application.Id).Msg("failed to load application during sync")
		failSyncJob(job.Application, log)
		completeSyncEvent(ctx, requestID, job.Application.Id, models.ApplicationEventFailed, "failed to load the application: "+err.Error(), log)
		return
	}
	composeconfig.CopySettings(&job.Application, &current)
	job.Application.SyncStatus = current.SyncStatus
	settingColumns := cfg.Apply(&job.Application)

	// A rolled back application runs an earlier compose file, so a new commit
	// is deployed even when it doesn't change the compose file. Changes of
	// x-orcacd alone are applied without deploying, unless the last deployment
	// didn't succeed.
	previousCompose := job.Application.ComposeFile.String()
	onlySettingsChanged := job.Application.SyncStatus == models.Synced && composeconfig.SameWithoutConfig(content, previousCompose)
	if (content == previousCompose || onlySettingsChanged) && job.Application.RolledBackAt == nil {
		now := time.Now()
		// Non-nil pointer to "" clears any previous error (GORM skips only nil pointers).
		cleared := ""
		source := job.Origin.Source
		job.Application.SyncStatus = models.Synced
		job.Application.Commit = job.Commit
		job.Application.CommitMessage = job.CommitMessage
		job.Application.LastSyncedAt = &now
		job.Application.LastSyncError = &cleared
		job.Application.LastSyncSource = &source
		columns := []string{"SyncStatus", "Commit", "CommitMessage", "LastSyncedAt", "LastSyncError", "LastSyncSource"}
		if content != previousCompose {
			job.Application.PreviousComposeFile = job.Application.ComposeFile
			job.Application.ComposeFile = crypto.EncryptedString(content)
			columns = append(columns, "ComposeFile", "PreviousComposeFile")
		}
		if err := saveApplication(ctx, &job.Application, append(columns, settingColumns...), log); err != nil {
			completeSyncEvent(ctx, requestID, job.Application.Id, models.ApplicationEventFailed, "failed to persist application sync status: "+err.Error(), log)
			return
		}
		if len(settingColumns) > 0 {
			hubws.SendAgentSettings(ctx, job.Application.AgentId, log)
		}
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

	job.Application.PreviousComposeFile = job.Application.ComposeFile
	job.Application.ComposeFile = crypto.EncryptedString(content)
	job.Application.Warnings = "[]"
	job.Application.Commit = job.Commit
	job.Application.CommitMessage = job.CommitMessage
	// The settings are saved before the deployment, which uses them, e.g. for
	// the automatic rollback.
	columns := append([]string{"ComposeFile", "Warnings", "PreviousComposeFile", "Commit", "CommitMessage"}, settingColumns...)
	if err := saveApplication(ctx, &job.Application, columns, log); err != nil {
		failSyncJob(job.Application, log)
		completeSyncEvent(ctx, requestID, job.Application.Id, models.ApplicationEventFailed, "failed to persist compose file: "+err.Error(), log)
		return
	}
	if len(settingColumns) > 0 {
		hubws.SendAgentSettings(ctx, job.Application.AgentId, log)
	}

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
	if origin.Source == models.ApplicationEventSourceRepositoryPolling && commit != "" && commit == application.Commit {
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

// saveApplication saves the columns of the application, which always
// include the settings that come from x-orcacd.
func saveApplication(ctx context.Context, application *models.Application, columns []string, log *zerolog.Logger) error {
	columns = append(columns, "ManagedSettings")
	_, err := gorm.G[models.Application](db.DB).
		Where("id = ?", application.Id).
		Select(columns[0], toAny(columns[1:])...).
		Updates(context.WithoutCancel(ctx), *application)
	if err != nil {
		log.Error().Err(err).Str("applicationId", application.Id).Msg("failed to save application")
	}
	sse.PublishUpdate(sse.TopicApplications.For(application.Id))
	return err
}

func toAny(values []string) []any {
	result := make([]any, len(values))
	for i, value := range values {
		result[i] = value
	}
	return result
}

// rejectedConfigs remembers the commit whose x-orcacd was invalid per
// application, so polls of that commit don't record the failure again.
var rejectedConfigs = rejectedConfigCommits{byApp: map[string]string{}}

type rejectedConfigCommits struct {
	sync.Mutex
	byApp map[string]string
}

func (r *rejectedConfigCommits) has(applicationID, commit string) bool {
	r.Lock()
	defer r.Unlock()
	return commit != "" && r.byApp[applicationID] == commit
}

// set remembers the commit, an empty one forgets it.
func (r *rejectedConfigCommits) set(applicationID, commit string) {
	r.Lock()
	defer r.Unlock()
	if commit == "" {
		delete(r.byApp, applicationID)
	} else {
		r.byApp[applicationID] = commit
	}
}

func failSyncJobWithError(application models.Application, message string, log *zerolog.Logger) {
	_ = updateApplicationStatus(context.Background(), application.Id, models.Application{
		SyncStatus:    models.OutOfSync,
		LastSyncError: &message,
	}, log)
	notifications.SendForApplication(application.Id, models.NotificationEventSyncFailed, "Error: sync failed for "+application.Name.String()+": "+message, log)
}

func failSyncJob(application models.Application, log *zerolog.Logger) {
	_ = updateApplicationStatus(context.Background(), application.Id, models.Application{
		SyncStatus: models.OutOfSync,
	}, log)
	notifications.SendForApplication(application.Id, models.NotificationEventSyncFailed, "Error: sync failed for "+application.Name.String(), log)
}
