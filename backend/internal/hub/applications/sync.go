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
}

func GetMatchingApplications(ctx context.Context, repository *models.Repository, branch string) ([]models.Application, error) {
	applications, err := gorm.G[models.Application](db.DB).Where("repository_id = ? AND branch = ?", repository.Id, branch).Find(ctx)
	if err != nil {
		return nil, err
	}
	return applications, nil
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

	// TODO: Do not fetch latest commit but specified commit
	// This requires to add a new method to the repositories.Provider interface to fetch commit info by commit hash
	commitInfo, err := job.RepositoryProvider.GetLatestCommit(ctx, &job.Repository, job.Application.Branch)
	if err != nil {
		log.Error().Err(err).Str("applicationId", job.Application.Id).
			Msg("failed to get latest commit info during sync")
		return
	}

	content, err := job.RepositoryProvider.GetFileContent(ctx, &job.Repository, job.Commit, job.Application.Path)
	if err != nil {
		log.Error().Err(err).Str("applicationId", job.Application.Id).
			Msg("failed to fetch compose file during sync")
		return
	}

	now := time.Now()

	if content == job.Application.ComposeFile.String() {
		// Compose file did not change
		success = true

		if _, err := gorm.G[models.Application](db.DB).
			Where("id = ?", job.Application.Id).
			Updates(ctx, models.Application{
				Commit:        job.Commit,
				CommitMessage: commitInfo.Message,
				SyncStatus:    models.Synced,
				LastSyncedAt:  &now,
			}); err != nil {
			log.Error().Err(err).Str("applicationId", job.Application.Id).
				Msg("failed to update application after sync (no compose change)")
		}
		sse.PublishUpdate("/api/v1/applications")
		return
	}

	// TODO: trigger actual deployment to agent
	// ...
	success = true

	if _, err := gorm.G[models.Application](db.DB).
		Where("id = ?", job.Application.Id).
		Updates(ctx, models.Application{
			ComposeFile:   crypto.EncryptedString(content),
			Commit:        job.Commit,
			CommitMessage: commitInfo.Message,
			SyncStatus:    models.Synced,
			LastSyncedAt:  &now,
		}); err != nil {
		log.Error().Err(err).Str("applicationId", job.Application.Id).
			Msg("failed to update application after sync (compose changed)")
	}

	sse.PublishUpdate("/api/v1/applications")
}
