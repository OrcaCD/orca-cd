package models

import (
	"time"

	"gorm.io/gorm"
)

type OneTimeAccessToken struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	Token     string         `gorm:"uniqueIndex;not null" json:"token"`
	UserID    uint           `gorm:"not null" json:"user_id"`
	User      User           `gorm:"foreignKey:UserID" json:"user,omitempty"`
	ExpiresAt time.Time      `gorm:"not null" json:"expires_at"`
	UsedAt    *time.Time     `json:"used_at,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"-"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// IsExpired checks if the token has expired
func (t *OneTimeAccessToken) IsExpired() bool {
	return time.Now().After(t.ExpiresAt)
}

// IsUsed checks if the token has been used
func (t *OneTimeAccessToken) IsUsed() bool {
	return t.UsedAt != nil
}

// IsValid checks if the token is valid (not expired and not used)
func (t *OneTimeAccessToken) IsValid() bool {
	return !t.IsExpired() && !t.IsUsed()
}
