package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Base struct {
	Id        string `gorm:"type:text;primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (a *Base) BeforeCreate(tx *gorm.DB) error {
	if a.Id == "" {
		newId, err := uuid.NewV7()
		if err != nil {
			return err
		}
		a.Id = newId.String()
	}
	a.CreatedAt = time.Now()
	a.UpdatedAt = time.Now()
	return nil
}
