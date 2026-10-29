package applicationevents

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"gorm.io/gorm"
)

const MaxPerApplication = 1000

type Params struct {
	ApplicationID string
	RequestID     *string
	Type          models.ApplicationEventType
	Source        models.ApplicationEventSource
	ActorUserID   *string
	ActorName     *string
	CommitHash    *string
	CommitMessage *string
	DeploymentID  *string
}

func Start(ctx context.Context, p Params) (*models.ApplicationEvent, error) {
	return create(ctx, p, models.ApplicationEventRunning, nil)
}

func RecordCompleted(
	ctx context.Context,
	p Params,
	status models.ApplicationEventStatus,
	errorMessage *string,
) (*models.ApplicationEvent, error) {
	if !isCompletedStatus(status) {
		return nil, fmt.Errorf("completed event cannot have running status")
	}
	return create(ctx, p, status, errorMessage)
}

func isCompletedStatus(status models.ApplicationEventStatus) bool {
	return status == models.ApplicationEventSucceeded ||
		status == models.ApplicationEventFailed ||
		status == models.ApplicationEventNoChange
}

func create(
	ctx context.Context,
	p Params,
	status models.ApplicationEventStatus,
	errorMessage *string,
) (*models.ApplicationEvent, error) {
	event := models.ApplicationEvent{
		ApplicationId: p.ApplicationID,
		RequestId:     p.RequestID,
		Type:          p.Type,
		Source:        p.Source,
		Status:        status,
		ActorUserId:   p.ActorUserID,
		ActorName:     p.ActorName,
		CommitHash:    p.CommitHash,
		CommitMessage: p.CommitMessage,
		ErrorMessage:  errorMessage,
		DeploymentId:  p.DeploymentID,
	}
	if status != models.ApplicationEventRunning {
		now := time.Now()
		event.CompletedAt = &now
	}

	// Keep creation and retention cleanup atomic so an error cannot leave an
	// inserted event after the caller was told that recording failed.
	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := gorm.G[models.ApplicationEvent](tx).Create(ctx, &event); err != nil {
			return err
		}
		return deleteEventsOverLimit(tx, p.ApplicationID)
	})
	if err != nil {
		return nil, err
	}

	// The activity endpoint never includes no_change events, so recording one
	// does not change it.
	publishUpdate(p.ApplicationID, status != models.ApplicationEventNoChange)
	return &event, nil
}

func publishUpdate(applicationID string, includeActivity bool) {
	if !includeActivity {
		sse.PublishUpdate(sse.TopicApplicationEvents(applicationID))
		return
	}
	sse.PublishUpdate(sse.TopicApplicationEvents(applicationID), sse.TopicApplicationActivity)
}

func deleteEventsOverLimit(tx *gorm.DB, applicationID string) error {
	return tx.Exec(`DELETE FROM application_events
			WHERE application_id = ? AND id NOT IN (
				SELECT id FROM application_events WHERE application_id = ?
				ORDER BY created_at DESC, id DESC LIMIT ?
			)`, applicationID, applicationID, MaxPerApplication).Error
}

func Complete(
	ctx context.Context,
	requestID string,
	applicationID string,
	status models.ApplicationEventStatus,
	errorMessage *string,
) (bool, error) {
	if !isCompletedStatus(status) {
		return false, fmt.Errorf("event completion requires a completed status")
	}

	now := time.Now()
	rowsAffected, err := gorm.G[models.ApplicationEvent](db.DB).
		Where(
			"request_id = ? AND application_id = ? AND status = ?",
			requestID,
			applicationID,
			models.ApplicationEventRunning,
		).
		Select("Status", "ErrorMessage", "CompletedAt").
		Updates(ctx, models.ApplicationEvent{
			Status:       status,
			ErrorMessage: errorMessage,
			CompletedAt:  &now,
		})
	if err != nil {
		return false, err
	}
	if rowsAffected != 1 {
		return false, nil
	}

	// Always refresh activity here, even for no_change: the running event was
	// listed there and has to disappear.
	publishUpdate(applicationID, true)
	return true, nil
}

// ForRequest returns the event recorded for requestID, or nil when no such
// event exists.
func ForRequest(ctx context.Context, requestID string, applicationID string) (*models.ApplicationEvent, error) {
	if requestID == "" {
		return nil, nil
	}
	event, err := gorm.G[models.ApplicationEvent](db.DB).
		Where("request_id = ? AND application_id = ?", requestID, applicationID).
		First(ctx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &event, nil
}

// SourceForRequest returns the source of the event recorded for requestID, or
// false when no such event exists (e.g. unsolicited Agent results).
func SourceForRequest(ctx context.Context, requestID string, applicationID string) (models.ApplicationEventSource, bool, error) {
	if requestID == "" {
		return "", false, nil
	}
	event, err := gorm.G[models.ApplicationEvent](db.DB).
		Select("source").
		Where("request_id = ? AND application_id = ?", requestID, applicationID).
		First(ctx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return event.Source, true, nil
}

func RecoverRunning(ctx context.Context, message string) (int64, error) {
	now := time.Now()
	rowsAffected, err := gorm.G[models.ApplicationEvent](db.DB).
		Where("status = ?", models.ApplicationEventRunning).
		Select("Status", "ErrorMessage", "CompletedAt").
		Updates(ctx, models.ApplicationEvent{
			Status:       models.ApplicationEventFailed,
			ErrorMessage: &message,
			CompletedAt:  &now,
		})
	return int64(rowsAffected), err
}
