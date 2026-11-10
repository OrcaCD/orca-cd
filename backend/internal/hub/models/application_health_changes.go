package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type HealthChangeReason string

const (
	// HealthChangeReasonAgentOffline marks health that became unknown because
	// the hub lost the connection to the application's agent.
	HealthChangeReasonAgentOffline HealthChangeReason = "agent_offline"
)

// ServiceHealth is the health of a single compose service at the time of a
// health change.
type ServiceHealth struct {
	Name   string       `json:"name"`
	Status HealthStatus `json:"status"`
}

// ApplicationHealthChange records a change of an application's health. Rows
// are only written when the status differs from the previous row, so the
// status of a row holds until the next row's ChangedAt.
type ApplicationHealthChange struct {
	Id            string              `gorm:"type:text;primaryKey"`
	ApplicationId string              `gorm:"type:text;not null;index:idx_application_health_changes_app_changed,priority:1"`
	Application   Application         `gorm:"foreignKey:ApplicationId;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`
	Status        HealthStatus        `gorm:"type:text;not null"`
	Reason        *HealthChangeReason `gorm:"type:text"`
	// Services holds the services that were not healthy, for incident context.
	Services  []ServiceHealth `gorm:"type:text;serializer:json"`
	ChangedAt time.Time       `gorm:"not null;index:idx_application_health_changes_app_changed,priority:2;index:idx_application_health_changes_changed"`
}

func (ApplicationHealthChange) TableName() string { return "application_health_changes" }

func (c *ApplicationHealthChange) BeforeCreate(tx *gorm.DB) error {
	if c.Id == "" {
		newId, err := uuid.NewV7()
		if err != nil {
			return err
		}
		c.Id = newId.String()
	}
	if c.ChangedAt.IsZero() {
		c.ChangedAt = time.Now()
	}
	// SQLite compares timestamps as text, which only orders correctly when
	// every row uses the same offset.
	c.ChangedAt = c.ChangedAt.UTC()
	return nil
}
