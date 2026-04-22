package applications

import (
	"context"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/repositories"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/rs/zerolog"
)

const defaultWorkerCount = 4
const maxQueueSize = defaultWorkerCount * 6

// TODO
// Prevent multiple concurrent syncs for the same application

type Queue struct {
	jobs    chan syncJob
	log     *zerolog.Logger
	workers int
}

var DefaultQueue *Queue

func NewQueue(log *zerolog.Logger) *Queue {
	workers := defaultWorkerCount
	return &Queue{
		jobs:    make(chan syncJob, maxQueueSize),
		log:     log,
		workers: workers,
	}
}

func (q *Queue) Start() {
	for range q.workers {
		go func() {
			for job := range q.jobs {
				processSyncJob(context.Background(), job, q.log)
			}
		}()
	}
}

func (q *Queue) Enqueue(ctx context.Context, repo *models.Repository, provider repositories.Provider, apps []models.Application, commit string) {
	for i := range apps {
		app := apps[i]

		resolvedCommit := commit
		if resolvedCommit == "" {
			commitInfo, err := provider.GetLatestCommit(ctx, repo, app.Branch)
			if err != nil {
				q.log.Error().Err(err).Str("applicationId", app.Id).
					Msg("failed to get latest commit for application sync")
				continue
			}
			resolvedCommit = commitInfo.Hash
		}

		job := syncJob{
			Application:        app,
			Repository:         *repo,
			RepositoryProvider: provider,
			Commit:             resolvedCommit,
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
