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
	log      *zerolog.Logger
	done     chan struct{}
	syncing  sync.Map // repo ID → struct{}, prevents concurrent syncs for the same repo
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup // tracks in-flight TriggerSync goroutines
	stopOnce sync.Once
}

func NewPoller(log *zerolog.Logger) *Poller {
	ctx, cancel := context.WithCancel(context.Background())
	return &Poller{
		log:    log,
		done:   make(chan struct{}),
		ctx:    ctx,
		cancel: cancel,
	}
}

func (p *Poller) Start() {
	go p.run()
}

// Stop cancels in-flight syncs and waits for them to finish.
func (p *Poller) Stop() {
	p.stopOnce.Do(func() {
		p.cancel()
		close(p.done)
		p.wg.Wait()
	})
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
	if db.DB == nil {
		return
	}
	if _, loaded := p.syncing.LoadOrStore(repo.Id, struct{}{}); loaded {
		return
	}
	repoCopy := *repo
	p.wg.Go(func() {
		defer p.syncing.Delete(repoCopy.Id)
		ctx, cancel := context.WithTimeout(p.ctx, 30*time.Second)
		defer cancel()
		SyncRepository(ctx, &repoCopy, p.log)
	})
}

// isDue reports whether a polling repository should be synced right now.
func isDue(repo *models.Repository, now time.Time) bool {
	if repo.PollingInterval == nil {
		return false
	}
	interval := *repo.PollingInterval
	if repo.LastSyncedAt == nil {
		return true
	}
	return now.Sub(*repo.LastSyncedAt) >= interval
}
