package githubactions

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

const triggerRetention = 24 * time.Hour

// Allow queued syncs to start, even when every worker ahead of them reaches
// its job deadline. Once an operation is recorded, its own lifecycle applies.
const operationRecordTimeout = 30 * time.Minute

const (
	running   = "running"
	failed    = "failed"
	succeeded = "succeeded"
	skipped   = "skipped"
)

type Result struct {
	Status       string              `json:"status"`
	Applications []ApplicationResult `json:"applications"`
}

type Application struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

type ApplicationResult struct {
	Application
	Status string        `json:"status"`
	Events []EventResult `json:"events"`
}

type EventResult struct {
	Type   models.ApplicationEventType   `json:"type"`
	Source models.ApplicationEventSource `json:"source,omitempty"`
	Status string                        `json:"status"`
	Commit *string                       `json:"commit,omitempty"`
	Error  *string                       `json:"error,omitempty"`
}

func GetResult(ctx context.Context, id, repository, runID string) (Result, error) {
	now := time.Now()
	trigger, err := gorm.G[models.GitHubActionsTrigger](db.DB).
		Where("id = ? AND repository = ? AND run_id = ? AND created_at > ?",
			id, strings.ToLower(repository), runID, now.Add(-triggerRetention)).
		First(ctx)
	if err != nil {
		return Result{}, err
	}

	events, err := gorm.G[models.ApplicationEvent](db.DB).
		Where("trigger_id = ?", id).Order("created_at, id").Find(ctx)
	if err != nil {
		return Result{}, err
	}
	skips, err := gorm.G[models.GitHubActionsTriggerSkip](db.DB).
		Where("trigger_id = ?", id).Order("type").Find(ctx)
	if err != nil {
		return Result{}, err
	}

	// Names are encrypted in the application table. Only IDs are kept on the trigger.
	apps, err := gorm.G[models.Application](db.DB).
		Select("id", "name").Where("id IN ?", trigger.ApplicationIds).Find(ctx)
	if err != nil {
		return Result{}, err
	}
	names := make(map[string]string, len(apps))
	for i := range apps {
		names[apps[i].Id] = apps[i].Name.String()
	}

	return buildResult(&trigger, events, skips, names, now), nil
}

func buildResult(trigger *models.GitHubActionsTrigger, events []models.ApplicationEvent, skips []models.GitHubActionsTriggerSkip, names map[string]string, now time.Time) Result {
	byApp := make(map[string][]EventResult, len(trigger.ApplicationIds))
	for i := range events {
		event := &events[i]
		byApp[event.ApplicationId] = append(byApp[event.ApplicationId], EventResult{
			Type: event.Type, Source: event.Source, Status: string(event.Status),
			Commit: event.CommitHash, Error: event.ErrorMessage,
		})
	}
	for _, skip := range skips {
		byApp[skip.ApplicationId] = append(byApp[skip.ApplicationId], EventResult{
			Type: skip.Type, Source: models.ApplicationEventSourceGitHubActions,
			Status: skipped, Error: new(skip.Reason),
		})
	}

	result := Result{Status: succeeded, Applications: make([]ApplicationResult, 0, len(trigger.ApplicationIds))}
	for _, id := range trigger.ApplicationIds {
		name, exists := names[id]
		if !exists {
			name = id
		}
		events := byApp[id]
		switch {
		case !exists:
			events = addMissingOperationFailures(trigger, events, "the application no longer exists")
		case !now.Before(trigger.CreatedAt.Add(operationRecordTimeout)):
			events = addMissingOperationFailures(trigger, events, "the Hub timed out waiting for an operation record")
		}
		if events == nil {
			events = []EventResult{}
		}
		status := applicationStatus(trigger, events)
		result.Applications = append(result.Applications, ApplicationResult{
			Id: id, Name: name, Status: status, Events: events,
		})
		switch {
		case status == running:
			result.Status = running
		case status == failed && result.Status != running:
			result.Status = failed
		}
	}
	return result
}

func addMissingOperationFailures(trigger *models.GitHubActionsTrigger, events []EventResult, reason string) []EventResult {
	for _, operation := range []struct {
		requested bool
		kind      models.ApplicationEventType
	}{
		{trigger.SyncRepo, models.ApplicationEventCommitSync},
		{trigger.PullImages, models.ApplicationEventImageUpdate},
	} {
		if operation.requested && !slices.ContainsFunc(events, func(event EventResult) bool { return event.Type == operation.kind }) {
			events = append(events, EventResult{
				Type: operation.kind, Source: models.ApplicationEventSourceGitHubActions, Status: failed, Error: &reason,
			})
		}
	}
	return events
}

func applicationStatus(trigger *models.GitHubActionsTrigger, events []EventResult) string {
	var sawSync, sawPull, sawFailure, sawSuccess bool
	for _, event := range events {
		if event.Status == running {
			return running
		}
		switch event.Type {
		case models.ApplicationEventCommitSync:
			sawSync = true
		case models.ApplicationEventImageUpdate:
			sawPull = true
		case models.ApplicationEventRollback:
			sawFailure = true
		}
		switch event.Status {
		case failed:
			sawFailure = true
		case succeeded, string(models.ApplicationEventNoChange):
			sawSuccess = true
		}
	}

	switch {
	case (trigger.SyncRepo && !sawSync) || (trigger.PullImages && !sawPull):
		return running
	case sawFailure:
		return failed
	case sawSuccess:
		return succeeded
	default:
		return skipped
	}
}

func StartPruner(log *zerolog.Logger) func() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			if err := prune(ctx, time.Now()); err != nil && ctx.Err() == nil {
				log.Error().Err(err).Msg("failed to prune expired GitHub Actions triggers")
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

func prune(ctx context.Context, now time.Time) error {
	_, err := gorm.G[models.GitHubActionsTrigger](db.DB).
		Where("created_at <= ?", now.Add(-triggerRetention)).Delete(ctx)
	return err
}
