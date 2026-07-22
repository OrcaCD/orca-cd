package applications

import (
	"context"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/repositories"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/OrcaCD/orca-cd/internal/shared/logger"
	"github.com/rs/zerolog"
)

var Log = logger.New("hub", false)

const defaultWorkerCount = 4
const maxQueueSize = defaultWorkerCount * 6
const jobTimeout = 3 * time.Minute

// Concurrent syncs for the same application are guarded at two levels: every
// sync entry point (poller, manual trigger, webhooks, GitHub Actions) claims
// the target repository's slot via Poller.TryLockRepositorySync before
// enqueuing jobs, and the agent serializes Deploy/Remove calls per
// application, so overlapping triggers cannot run docker compose concurrently
// against the same project.

type Queue struct {
	jobs    chan syncJob
	log     *zerolog.Logger
	workers int
}

var DefaultQueue *Queue

func NewQueue(log *zerolog.Logger) *Queue {
	return &Queue{
		jobs:    make(chan syncJob, maxQueueSize),
		log:     log,
		workers: defaultWorkerCount,
	}
}

func (q *Queue) Start() {
	for range q.workers {
		go func() {
			for job := range q.jobs {
				ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
				processSyncJob(ctx, job, q.log)
				cancel()
			}
		}()
	}
}

func (q *Queue) Enqueue(repo *models.Repository, provider repositories.Provider, apps []models.Application, commit, commitMessage string, origin SyncOrigin) {
	for _, app := range apps {
		job := syncJob{
			Application:        app,
			Repository:         *repo,
			RepositoryProvider: provider,
			Commit:             commit,
			CommitMessage:      commitMessage,
			Origin:             origin,
		}

		select {
		case q.jobs <- job:
		default:
			q.log.Warn().Str("applicationId", app.Id).Msg("sync queue full, dropping job")
			recordSyncFailure(context.Background(), &app, origin, commit, commitMessage, "sync queue full, job dropped", q.log)
		}
	}

	if len(apps) > 0 {
		sse.PublishUpdate("/api/v1/applications")
	}
}
