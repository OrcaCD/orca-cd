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

func SyncRepository(ctx context.Context, repo *models.Repository, log *zerolog.Logger) {
	provider, err := repositories.Get(repo.Provider)
	if err != nil {
		log.Error().Err(err).Str("repositoryId", repo.Id).Msg("unsupported provider for sync")
		markRepositoryFailed(ctx, repo.Id, "unsupported provider", log)
		return
	}

	markRepositorySyncing(ctx, repo.Id, log)

	apps, err := gorm.G[models.Application](db.DB).Where("repository_id = ?", repo.Id).Find(ctx)
	if err != nil {
		log.Error().Err(err).Str("repositoryId", repo.Id).Msg("failed to load applications for sync")
		markRepositoryFailed(ctx, repo.Id, "failed to load applications", log)
		return
	}

	if len(apps) == 0 {
		now := time.Now()
		markRepositorySuccess(ctx, repo.Id, &now, log)
		return
	}

	// Group apps by branch and resolve the latest commit for each branch up front.
	// This ensures the repository status reflects real connectivity, and passes an
	// explicit commit hash to the queue (avoiding a redundant GetLatestCommit call).
	byBranch := make(map[string][]models.Application)
	for i := range apps {
		branch := apps[i].Branch
		if branch == "" {
			continue
		}
		byBranch[branch] = append(byBranch[branch], apps[i])
	}

	now := time.Now()
	var lastErrMsg string
	for branch, branchApps := range byBranch {
		commitInfo, err := provider.GetLatestCommit(ctx, repo, branch)
		if err != nil {
			lastErrMsg = fmt.Sprintf("failed to get latest commit for branch %q: %v", branch, err)
			log.Error().Err(err).Str("repositoryId", repo.Id).Str("branch", branch).Msg("failed to get latest commit during sync")
			continue
		}
		DefaultQueue.Enqueue(repo, provider, branchApps, commitInfo.Hash, commitInfo.Message)
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
