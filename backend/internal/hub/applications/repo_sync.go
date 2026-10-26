package applications

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/commitsigning"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/notifications"
	"github.com/OrcaCD/orca-cd/internal/hub/repositories"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

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
		// Polls of an already deployed commit are dropped by the queue, so they
		// skip the extra provider request. Every commit that may be deployed is verified.
		if origin.Source != models.ApplicationEventSourceRepositoryPolling || !allAtCommit(branchApps, hash) {
			if repeated, err := verifySyncCommit(ctx, provider, repo, branch, hash, origin); err != nil {
				lastErrMsg = err.Error()
				if repeated {
					continue
				}
				log.Warn().Err(err).Str("repositoryId", repo.Id).Str("branch", branch).Msg("rejected commit during sync")
				for i := range branchApps {
					recordSyncFailure(ctx, &branchApps[i], origin, hash, message, lastErrMsg, log)
				}
				continue
			}
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

type rejectedCommit struct {
	hash string
	keys [sha256.Size]byte
	err  error
}

// rejectedCommits remembers the last commit rejected for unsigned or untrusted
// signatures per repository branch. Polls of that commit neither call the
// provider again nor add another failed event to the history. Changing the
// trusted keys invalidates the entry; a hub restart records the rejection once more.
var rejectedCommits = struct {
	sync.Mutex
	byBranch map[string]rejectedCommit
}{byBranch: map[string]rejectedCommit{}}

// verifySyncCommit enforces the signed commit policy and reports whether a
// rejection was already recorded by an earlier poll.
func verifySyncCommit(ctx context.Context, provider repositories.Provider, repo *models.Repository, branch, hash string, origin SyncOrigin) (repeated bool, err error) {
	if !repo.RequireSignedCommits {
		return false, nil
	}
	key := repo.Id + "\x00" + branch
	keysDigest := sha256.Sum256([]byte(strings.Join(repo.CommitSigningKeys, "\x00")))

	rejectedCommits.Lock()
	rejected, ok := rejectedCommits.byBranch[key]
	rejectedCommits.Unlock()
	if ok && origin.Source == models.ApplicationEventSourceRepositoryPolling && rejected.hash == hash && rejected.keys == keysDigest {
		return true, rejected.err
	}

	err = repositories.VerifyCommit(ctx, provider, repo, hash)
	rejectedCommits.Lock()
	defer rejectedCommits.Unlock()
	switch {
	case errors.Is(err, commitsigning.ErrUnsigned), errors.Is(err, commitsigning.ErrUntrustedSigner):
		// The signature of a commit never changes, so the result only
		// depends on the trusted keys. Other errors may be transient.
		rejectedCommits.byBranch[key] = rejectedCommit{hash: hash, keys: keysDigest, err: err}
	case err == nil:
		delete(rejectedCommits.byBranch, key)
	}
	return false, err
}

func allAtCommit(apps []models.Application, hash string) bool {
	for i := range apps {
		if apps[i].Commit != hash {
			return false
		}
	}
	return true
}

func markRepositorySyncing(ctx context.Context, id string, log *zerolog.Logger) {
	if _, err := gorm.G[models.Repository](db.DB).Where("id = ?", id).
		Updates(ctx, models.Repository{SyncStatus: models.SyncStatusSyncing}); err != nil {
		log.Error().Err(err).Str("repositoryId", id).Msg("failed to mark repository as syncing")
	}
	sse.PublishUpdate(sse.TopicRepositories)
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
	sse.PublishUpdate(sse.TopicRepositories)

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
	sse.PublishUpdate(sse.TopicRepositories)

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
// (e.g. every polling interval) and concurrent syncs notify at most once. Only
// if that matches nothing is the result stored unconditionally. A single update
// cannot do both: its affected rows do not reveal the previous state, and
// SQLite's RETURNING only sees the new values.
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
