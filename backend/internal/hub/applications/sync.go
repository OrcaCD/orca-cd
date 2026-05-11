package applications

import (
	"context"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
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

func processSyncJob(ctx context.Context, job syncJob, log *zerolog.Logger) {
	if _, err := gorm.G[models.Application](db.DB).
		Where("id = ?", job.Application.Id).
		Updates(ctx, models.Application{
			SyncStatus: models.Syncing,
		}); err != nil {
		log.Error().Err(err).Str("applicationId", job.Application.Id).
			Msg("failed to set application sync status to syncing")
	}
	sse.PublishUpdate("/api/v1/applications")

	success := false
	defer func() {
		if !success {
			if _, err := gorm.G[models.Application](db.DB).
				Where("id = ?", job.Application.Id).
				Updates(ctx, models.Application{
					SyncStatus: models.OutOfSync,
				}); err != nil {
				log.Error().Err(err).Str("applicationId", job.Application.Id).
					Msg("failed to mark application as out_of_sync after failed sync")
			}
			sse.PublishUpdate("/api/v1/applications")
		}
	}()

	content, err := job.RepositoryProvider.GetFileContent(ctx, &job.Repository, job.Commit, job.Application.Path)
	if err != nil {
		log.Error().Err(err).Str("applicationId", job.Application.Id).
			Msg("failed to fetch compose file during sync")
		return
	}

	now := time.Now()

	if content == job.Application.ComposeFile.String() {
		// No changes in compose file, just update commit and sync status
		success = true
		if _, err := gorm.G[models.Application](db.DB).
			Where("id = ?", job.Application.Id).
			Updates(ctx, models.Application{
				Commit:        job.Commit,
				CommitMessage: job.CommitMessage,
				SyncStatus:    models.Synced,
				LastSyncedAt:  &now,
			}); err != nil {
			log.Error().Err(err).Str("applicationId", job.Application.Id).
				Msg("failed to update application after sync (no compose change)")
		}
		sse.PublishUpdate("/api/v1/applications")
		return
	}

	if DefaultDeployer == nil {
		log.Error().Str("applicationId", job.Application.Id).Msg("application deployer not initialized")
		return
	}

	result, err := DefaultDeployer.DeployAndWait(ctx, &job.Application, content)
	if err != nil {
		log.Error().Err(err).Str("applicationId", job.Application.Id).Msg("failed to deploy compose file to agent")
		return
	}
	if result == nil {
		log.Error().Str("applicationId", job.Application.Id).Msg("agent deployment finished without a result")
		return
	}
	if !result.Success {
		log.Error().
			Str("applicationId", job.Application.Id).
			Str("request_id", result.RequestId).
			Str("error", result.ErrorMessage).
			Msg("agent reported deployment failure")
		markDeploymentExecutionFailure(ctx, job.Application.Id, log)
		return
	}

	success = true
	markDeploymentSuccess(ctx, job.Application.Id, func(update *models.Application) {
		update.ComposeFile = crypto.EncryptedString(content)
		update.PreviousComposeFile = job.Application.ComposeFile
		update.Commit = job.Commit
		update.CommitMessage = job.CommitMessage
		update.LastSyncedAt = &now
	}, log)
}
