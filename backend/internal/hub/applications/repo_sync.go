package applications

import (
	"context"
	"fmt"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/repositories"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

const repositoriesSSEPath = "/api/v1/repositories"

type CommitResolver func(ctx context.Context, branch string) (hash, message string, err error)

func StaticCommit(hash, message string) CommitResolver {
	return func(context.Context, string) (string, string, error) {
		return hash, message, nil
	}
}

func LatestCommit(provider repositories.Provider, repo *models.Repository) CommitResolver {
	return func(ctx context.Context, branch string) (string, string, error) {
		info, err := provider.GetLatestCommit(ctx, repo, branch)
		if err != nil {
			return "", "", err
		}
		return info.Hash, info.Message, nil
	}
}

func SyncRepository(ctx context.Context, repo *models.Repository, log *zerolog.Logger) {
	provider, err := repositories.Get(repo.Provider)
	if err != nil {
		log.Error().Err(err).Str("repositoryId", repo.Id).Msg("unsupported provider for sync")
		markRepositoryFailed(ctx, repo.Id, "unsupported provider", log)
		return
	}

	apps, err := gorm.G[models.Application](db.DB).Where("repository_id = ?", repo.Id).Find(ctx)
	if err != nil {
		log.Error().Err(err).Str("repositoryId", repo.Id).Msg("failed to load applications for sync")
		markRepositoryFailed(ctx, repo.Id, "failed to load applications", log)
		return
	}

	SyncApplications(ctx, repo, provider, apps, LatestCommit(provider, repo), log)
}

func SyncApplications(ctx context.Context, repo *models.Repository, provider repositories.Provider, apps []models.Application, resolve CommitResolver, log *zerolog.Logger) {
	markRepositorySyncing(ctx, repo.Id, log)

	byBranch := make(map[string][]models.Application)
	for i := range apps {
		if branch := apps[i].Branch; branch != "" {
			byBranch[branch] = append(byBranch[branch], apps[i])
		}
	}

	now := time.Now()
	if len(byBranch) == 0 {
		markRepositorySuccess(ctx, repo.Id, &now, log)
		return
	}

	var lastErrMsg string
	for branch, branchApps := range byBranch {
		hash, message, err := resolve(ctx, branch)
		if err != nil {
			lastErrMsg = fmt.Sprintf("failed to resolve commit for branch %q: %v", branch, err)
			log.Error().Err(err).Str("repositoryId", repo.Id).Str("branch", branch).Msg("failed to resolve commit during sync")
			continue
		}
		if DefaultQueue != nil {
			DefaultQueue.Enqueue(repo, provider, branchApps, hash, message)
		} else {
			log.Error().Str("repositoryId", repo.Id).Str("branch", branch).Msg("sync queue not initialized")
			markRepositoryFailed(ctx, repo.Id, "sync queue not initialized", log)
			return
		}
	}

	if lastErrMsg != "" {
		markRepositoryFailed(ctx, repo.Id, lastErrMsg, log)
		return
	}
	markRepositorySuccess(ctx, repo.Id, &now, log)
}

func markRepositorySyncing(ctx context.Context, id string, log *zerolog.Logger) {
	if _, err := gorm.G[models.Repository](db.DB).Where("id = ?", id).
		Updates(ctx, models.Repository{SyncStatus: models.SyncStatusSyncing}); err != nil {
		log.Error().Err(err).Str("repositoryId", id).Msg("failed to mark repository as syncing")
	}
	sse.PublishUpdate(repositoriesSSEPath)
}

func markRepositorySuccess(ctx context.Context, id string, now *time.Time, log *zerolog.Logger) {
	if _, err := gorm.G[models.Repository](db.DB).Where("id = ?", id).
		Updates(ctx, models.Repository{
			SyncStatus:    models.SyncStatusSuccess,
			LastSyncError: nil,
			LastSyncedAt:  now,
		}); err != nil {
		log.Error().Err(err).Str("repositoryId", id).Msg("failed to mark repository as success")
	}
	sse.PublishUpdate(repositoriesSSEPath)
}

func markRepositoryFailed(ctx context.Context, id string, errMsg string, log *zerolog.Logger) {
	if _, err := gorm.G[models.Repository](db.DB).Where("id = ?", id).
		Updates(ctx, models.Repository{
			SyncStatus:    models.SyncStatusFailed,
			LastSyncError: &errMsg,
		}); err != nil {
		log.Error().Err(err).Str("repositoryId", id).Msg("failed to mark repository as failed")
	}
	sse.PublishUpdate(repositoriesSSEPath)
}
