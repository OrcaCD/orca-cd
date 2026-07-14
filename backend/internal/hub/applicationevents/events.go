package applicationevents

import (
	"context"
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
}

func Path(applicationID string) string {
	return "/api/v1/applications/" + applicationID + "/events"
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

	sse.PublishUpdate(Path(p.ApplicationID))
	return &event, nil
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

	sse.PublishUpdate(Path(applicationID))
	return true, nil
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
