package applications

import (
	"context"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	application_deployer "github.com/OrcaCD/orca-cd/internal/hub/deployer"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/repositories"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
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

// processSyncJob performs the application-level half of a sync for a single app:
// fetch the compose file at the resolved commit, and if it changed, deploy it to
// the agent and wait for the result. It is the unit of work the queue workers run,
// and it owns the application's sync-status transitions (Syncing → Synced/OutOfSync).
func processSyncJob(ctx context.Context, job syncJob, log *zerolog.Logger) {
	content, err := job.RepositoryProvider.GetFileContent(ctx, &job.Repository, job.Commit, job.Application.Path)
	if err != nil {
		log.Error().Err(err).Str("applicationId", job.Application.Id).
			Msg("failed to fetch compose file during sync")
		// failSyncJob(job.Application, log)
		return
	}

	if content == job.Application.ComposeFile.String() {
		// Compose file unchanged: record the new commit and mark synced without redeploying.
		_ = updateApplicationStatus(context.Background(), job.Application.Id, models.Application{
			Commit:        job.Commit,
			CommitMessage: job.CommitMessage,
		}, log)
		return
	}

	deployer := application_deployer.DefaultApplicationDeployer
	if deployer == nil {
		log.Error().Str("applicationId", job.Application.Id).Msg("application deployer not initialized")
		// failSyncJob(job.Application, log)
		return
	}

	if err := deployer.TriggerApplicationDeploy(context.Background(), &job.Application, content); err != nil {
		log.Error().Err(err).Str("applicationId", job.Application.Id).Msg("failed to trigger application deploy")
		// failSyncJob(job.Application, log)
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
