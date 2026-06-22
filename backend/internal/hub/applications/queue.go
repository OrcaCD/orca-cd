package applications

import (
	"context"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/repositories"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/rs/zerolog"
)

const defaultWorkerCount = 4
const maxQueueSize = defaultWorkerCount * 6
const jobTimeout = 3 * time.Minute

// TODO
// Prevent multiple concurrent syncs for the same application

// Queue decouples repository-level fan-out from application-level deploys. The
// repository side (SyncRepository, webhooks, GitHub Actions) enqueues one job per
// application; a fixed pool of workers then runs processSyncJob for each job
// concurrently, so a slow deploy on one application does not block the others.
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

// QueueLogger returns the sync queue's logger, or a no-op logger if the queue has
// not been initialised. Route handlers have no logger of their own, so they use
// this to pass one to SyncApplications.
func QueueLogger() *zerolog.Logger {
	if DefaultQueue != nil {
		return DefaultQueue.log
	}
	nop := zerolog.Nop()
	return &nop
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

func (q *Queue) Enqueue(repo *models.Repository, provider repositories.Provider, apps []models.Application, commit, commitMessage string) {
	for _, app := range apps {
		job := syncJob{
			Application:        app,
			Repository:         *repo,
			RepositoryProvider: provider,
			Commit:             commit,
			CommitMessage:      commitMessage,
		}

		select {
		case q.jobs <- job:
		default:
			q.log.Warn().Str("applicationId", app.Id).Msg("sync queue full, dropping job")
		}
	}

	if len(apps) > 0 {
		sse.PublishUpdate("/api/v1/applications")
	}
}
