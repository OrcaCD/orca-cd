package applications

import (
	"context"
	"time"

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

type syncJob struct {
	Application        models.Application
	Repository         models.Repository
	RepositoryProvider repositories.Provider
	Commit             string
	CommitMessage      string
}

func GetMatchingApplications(ctx context.Context, repository *models.Repository, branch string) ([]models.Application, error) {
	return gorm.G[models.Application](db.DB).Where("repository_id = ? AND branch = ?", repository.Id, branch).Find(ctx)
}

func GetAllApplicationsForRepo(ctx context.Context, repository *models.Repository) ([]models.Application, error) {
	return gorm.G[models.Application](db.DB).Where("repository_id = ?", repository.Id).Find(ctx)
}

func processSyncJob(ctx context.Context, job syncJob, log *zerolog.Logger) {
	content, err := job.RepositoryProvider.GetFileContent(ctx, &job.Repository, job.Commit, job.Application.Path)
	if err != nil {
		log.Error().Err(err).Str("applicationId", job.Application.Id).
			Msg("failed to fetch compose file during sync")
		failSyncJob(job.Application, log)
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
		return
	}

	deployer := application_deployer.DefaultApplicationDeployer
	if deployer == nil {
		log.Error().Str("applicationId", job.Application.Id).Msg("application deployer not initialized")
		failSyncJob(job.Application, log)
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
		return
	}

	if err := deployer.TriggerApplicationDeploy(context.Background(), &job.Application, content, uuid.NewString()); err != nil {
		log.Error().Err(err).Str("applicationId", job.Application.Id).Msg("failed to trigger application deploy")
		failSyncJob(job.Application, log)
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
