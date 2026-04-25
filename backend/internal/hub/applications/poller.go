package applications

import (
	"context"
	"sync"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

const pollerCheckInterval = 30 * time.Second

var DefaultPoller *Poller

type Poller struct {
	log     *zerolog.Logger
	done    chan struct{}
	syncing sync.Map // repo ID → struct{}, prevents concurrent syncs for the same repo
}

func NewPoller(log *zerolog.Logger) *Poller {
	return &Poller{
		log:  log,
		done: make(chan struct{}),
	}
}

func (p *Poller) Start() {
	go p.run()
}

func (p *Poller) Stop() {
	close(p.done)
}

func (p *Poller) run() {
	ticker := time.NewTicker(pollerCheckInterval)
	defer ticker.Stop()
	p.pollRepositories()
	for {
		select {
		case <-ticker.C:
			p.pollRepositories()
		case <-p.done:
			return
		}
	}
}

func (p *Poller) pollRepositories() {
	if db.DB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repos, err := gorm.G[models.Repository](db.DB).
		Where("sync_type = ?", models.SyncTypePolling).
		Find(ctx)
	if err != nil {
		p.log.Error().Err(err).Msg("poller: failed to list polling repositories")
		return
	}
	now := time.Now()
	for i := range repos {
		if isDue(&repos[i], now) {
			p.TriggerSync(&repos[i])
		}
	}
}

// TriggerSync initiates an async sync for the given repository. If a sync for this
// repository is already in progress it is silently skipped.
func (p *Poller) TriggerSync(repo *models.Repository) {
	if _, loaded := p.syncing.LoadOrStore(repo.Id, struct{}{}); loaded {
		return
	}
	repoCopy := *repo
	go func() {
		defer p.syncing.Delete(repoCopy.Id)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		SyncRepository(ctx, &repoCopy, p.log)
	}()
}

// isDue reports whether a polling repository should be synced right now.
func isDue(repo *models.Repository, now time.Time) bool {
	if repo.PollingInterval == nil {
		return false
	}
	interval := time.Duration(*repo.PollingInterval) * time.Second
	if repo.LastSyncedAt == nil {
		return true
	}
	return now.Sub(*repo.LastSyncedAt) >= interval
}
