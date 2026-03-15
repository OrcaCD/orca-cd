package models

import (
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type Agent struct {
	ID        string                 `gorm:"type:text;primaryKey"`
	Name      crypto.EncryptedString `gorm:"type:text;not null"`
	Secret    crypto.EncryptedString `gorm:"type:text;not null"` // bcrypt hash, still encrypted at rest
	Status    string                 `gorm:"type:text;default:'registered'"`
	Metadata  datatypes.JSON         `gorm:"type:text"` // encrypt separately if sensitive
	LastSeen  *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (a *Agent) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	return nil
}
