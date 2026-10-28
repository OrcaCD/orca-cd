// Package healthhistory records application health changes and keeps them
// for the availability statistics.
package healthhistory

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// Retention is how long health changes are kept. The longest statistics range
// must fit into it.
const Retention = 30 * 24 * time.Hour

const (
	pruneInterval = 24 * time.Hour
	// Services and their names come from agents, so both are bounded before
	// they are persisted.
	maxServices          = 32
	maxServiceNameLength = 128
)

// Record stores a health change unless the application's latest recorded
// status is the same. A change dated before the latest one is moved to the
// latest change's time so the history stays ordered. The very first change of
// an application is not recorded when it is unknown, as no data is implied
// before the first change anyway.
//
// Callers must serialize changes per application; the hub does so by
// handling each agent's messages sequentially.
func Record(ctx context.Context, change models.ApplicationHealthChange) (bool, error) {
	if change.ChangedAt.IsZero() {
		change.ChangedAt = time.Now()
	}

	latest, err := Latest(ctx, change.ApplicationId)
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		if change.Status == models.UnknownHealth {
			return false, nil
		}
	case err != nil:
		return false, err
	case latest.Status == change.Status:
		return false, nil
	case change.ChangedAt.Before(latest.ChangedAt):
		change.ChangedAt = latest.ChangedAt
	}

	change.Services = sanitizeServices(change.Services)
	if err := gorm.G[models.ApplicationHealthChange](db.DB).Create(ctx, &change); err != nil {
		return false, err
	}
	return true, nil
}

// Latest returns the most recent health change of an application.
func Latest(ctx context.Context, applicationID string) (models.ApplicationHealthChange, error) {
	return gorm.G[models.ApplicationHealthChange](db.DB).
		Where("application_id = ?", applicationID).
		Order("changed_at DESC, id DESC").
		First(ctx)
}

// RecordAgentOffline marks the health of all applications of an agent as
// unknown because the agent is no longer connected.
func RecordAgentOffline(ctx context.Context, agentID string, at time.Time) error {
	apps, err := gorm.G[models.Application](db.DB).Select("id").Where("agent_id = ?", agentID).Find(ctx)
	if err != nil {
		return err
	}

	reason := models.HealthChangeReasonAgentOffline
	var errs []error
	for _, app := range apps {
		if _, err := Record(ctx, models.ApplicationHealthChange{
			ApplicationId: app.Id,
			Status:        models.UnknownHealth,
			Reason:        &reason,
			ChangedAt:     at,
		}); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Prune deletes health changes older than the retention. The latest change
// before the cutoff is kept per application, as it defines the status at the
// start of the longest range.
func Prune(ctx context.Context, now time.Time) (int, error) {
	cutoff := now.Add(-Retention).UTC()
	return gorm.G[models.ApplicationHealthChange](db.DB).
		Where(`changed_at < ? AND id NOT IN (
			SELECT id FROM (
				SELECT id, ROW_NUMBER() OVER (PARTITION BY application_id ORDER BY changed_at DESC, id DESC) AS position
				FROM application_health_changes
				WHERE changed_at < ?
			) WHERE position = 1
		)`, cutoff, cutoff).
		Delete(ctx)
}

// StartPruner prunes the health history now and then once a day. The returned
// stop function cancels a running prune and waits for the pruner to exit, so
// the database can be closed afterwards.
func StartPruner(log *zerolog.Logger) (stop func()) {
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	prune := func() {
		pruneCtx, cancelPrune := context.WithTimeout(ctx, time.Minute)
		defer cancelPrune()
		deleted, err := Prune(pruneCtx, time.Now())
		if err != nil {
			if ctx.Err() == nil {
				log.Error().Err(err).Msg("failed to prune application health history")
			}
			return
		}
		if deleted > 0 {
			log.Debug().Int("count", deleted).Msg("pruned application health history")
		}
	}

	go func() {
		defer close(stopped)
		prune()
		ticker := time.NewTicker(pruneInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				prune()
			case <-ctx.Done():
				return
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			<-stopped
		})
	}
}

func sanitizeServices(services []models.ServiceHealth) []models.ServiceHealth {
	if len(services) == 0 {
		return nil
	}
	services = services[:min(len(services), maxServices)]
	sanitized := make([]models.ServiceHealth, 0, len(services))
	for _, service := range services {
		name := strings.ToValidUTF8(service.Name, "")
		name = strings.TrimSpace(truncate(strings.TrimSpace(name), maxServiceNameLength))
		sanitized = append(sanitized, models.ServiceHealth{Name: name, Status: service.Status})
	}
	return sanitized
}

// truncate cuts s to at most maxBytes without splitting a UTF-8 character.
func truncate(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	for maxBytes > 0 && !utf8.RuneStart(s[maxBytes]) {
		maxBytes--
	}
	return s[:maxBytes]
}
