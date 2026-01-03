package models

import (
	"time"

	"gorm.io/gorm"
)

type Message struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	Text      string         `gorm:"not null" json:"text"`
	CreatedAt time.Time      `json:"timestamp"`
	UpdatedAt time.Time      `json:"-"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}
