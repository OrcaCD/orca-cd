package applications

import (
	"context"
	"fmt"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/notifications"
	"github.com/OrcaCD/orca-cd/internal/hub/repositories"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

const repositoriesSSEPath = "/api/v1/repositories"

var sendForRepository = notifications.SendForRepository

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

func SyncRepository(ctx context.Context, repo *models.Repository, origin SyncOrigin, log *zerolog.Logger) {
	provider, err := repositories.Get(repo.Provider)
	if err != nil {
		log.Error().Err(err).Str("repositoryId", repo.Id).Msg("unsupported provider for sync")
		markRepositoryFailed(ctx, repo, "unsupported provider", log)
		return
	}

	apps, err := gorm.G[models.Application](db.DB).Where("repository_id = ?", repo.Id).Find(ctx)
	if err != nil {
		log.Error().Err(err).Str("repositoryId", repo.Id).Msg("failed to load applications for sync")
		markRepositoryFailed(ctx, repo, "failed to load applications", log)
		return
	}

	SyncApplications(ctx, repo, provider, apps, LatestCommit(provider, repo), origin, log)
}

func SyncApplications(ctx context.Context, repo *models.Repository, provider repositories.Provider, apps []models.Application, resolve CommitResolver, origin SyncOrigin, log *zerolog.Logger) {
	markRepositorySyncing(ctx, repo.Id, log)

	byBranch := make(map[string][]models.Application)
	for i := range apps {
		if branch := apps[i].Branch; branch != "" {
			byBranch[branch] = append(byBranch[branch], apps[i])
		}
	}

	now := time.Now()
	if len(byBranch) == 0 {
		markRepositorySuccess(ctx, repo, &now, log)
		return
	}

	var lastErrMsg string
	for branch, branchApps := range byBranch {
		hash, message, err := resolve(ctx, branch)
		if err != nil {
			lastErrMsg = fmt.Sprintf("failed to resolve commit for branch %q: %v", branch, err)
			log.Error().Err(err).Str("repositoryId", repo.Id).Str("branch", branch).Msg("failed to resolve commit during sync")
			for i := range branchApps {
				recordSyncFailure(ctx, &branchApps[i], origin, "", "", lastErrMsg, log)
			}
			continue
		}
		if DefaultQueue != nil {
			DefaultQueue.Enqueue(repo, provider, branchApps, hash, message, origin)
		} else {
			log.Error().Str("repositoryId", repo.Id).Str("branch", branch).Msg("sync queue not initialized")
			markRepositoryFailed(ctx, repo, "sync queue not initialized", log)
			for i := range branchApps {
				recordSyncFailure(ctx, &branchApps[i], origin, hash, message, "sync queue not initialized", log)
			}
			return
		}
	}

	if lastErrMsg != "" {
		markRepositoryFailed(ctx, repo, lastErrMsg, log)
		return
	}
	markRepositorySuccess(ctx, repo, &now, log)
}

func markRepositorySyncing(ctx context.Context, id string, log *zerolog.Logger) {
	if _, err := gorm.G[models.Repository](db.DB).Where("id = ?", id).
		Updates(ctx, models.Repository{SyncStatus: models.SyncStatusSyncing}); err != nil {
		log.Error().Err(err).Str("repositoryId", id).Msg("failed to mark repository as syncing")
	}
	sse.PublishUpdate(repositoriesSSEPath)
}

func markRepositorySuccess(ctx context.Context, repo *models.Repository, now *time.Time, log *zerolog.Logger) {
	recovered, err := updateRepositorySyncResult(repo.Id, "last_sync_error IS NOT NULL",
		func(q gorm.ChainInterface[models.Repository]) (int, error) {
			// Select forces the nil LastSyncError to be written as NULL, clearing any
			// stale error (a struct Updates would otherwise skip the nil pointer).
			return q.Select("SyncStatus", "LastSyncError", "LastSyncedAt").
				Updates(ctx, models.Repository{
					SyncStatus:    models.SyncStatusSuccess,
					LastSyncError: nil,
					LastSyncedAt:  now,
				})
		})
	if err != nil {
		log.Error().Err(err).Str("repositoryId", repo.Id).Msg("failed to mark repository as success")
	}
	sse.PublishUpdate(repositoriesSSEPath)

	if recovered {
		sendForRepository(repo.Id, models.NotificationEventRepositorySyncRecovered,
			"Success: sync recovered for repository "+repo.Name, log)
	}
}

func markRepositoryFailed(ctx context.Context, repo *models.Repository, errMsg string, log *zerolog.Logger) {
	failed, err := updateRepositorySyncResult(repo.Id, "last_sync_error IS NULL",
		func(q gorm.ChainInterface[models.Repository]) (int, error) {
			return q.Updates(ctx, models.Repository{
				SyncStatus:    models.SyncStatusFailed,
				LastSyncError: &errMsg,
			})
		})
	if err != nil {
		log.Error().Err(err).Str("repositoryId", repo.Id).Msg("failed to mark repository as failed")
	}
	sse.PublishUpdate(repositoriesSSEPath)

	if failed {
		sendForRepository(repo.Id, models.NotificationEventRepositorySyncFailed,
			"Error: sync failed for repository "+repo.Name+": "+truncateNotificationDetail(errMsg), log)
	}
}

// updateRepositorySyncResult stores a sync result and reports whether it
// changed the repository between failing and succeeding. The sync status is
// useless for that, since markRepositorySyncing overwrites it before every
// sync, but last_sync_error survives until the next successful sync. The
// transition is claimed with a conditional update first, so repeated results
// (e.g. every polling interval) and concurrent syncs notify at most once.
func updateRepositorySyncResult(id, transition string, update func(gorm.ChainInterface[models.Repository]) (int, error)) (bool, error) {
	rowsAffected, err := update(gorm.G[models.Repository](db.DB).Where("id = ?", id).Where(transition))
	if err != nil {
		return false, err
	}
	if rowsAffected > 0 {
		return true, nil
	}

	_, err = update(gorm.G[models.Repository](db.DB).Where("id = ?", id))
	return false, err
}

const maxNotificationDetailLength = 500

func truncateNotificationDetail(detail string) string {
	runes := []rune(detail)
	if len(runes) <= maxNotificationDetailLength {
		return detail
	}
	return string(runes[:maxNotificationDetailLength]) + "…"
}
