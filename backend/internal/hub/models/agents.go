package models

import (
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
)

type AgentStatus int

const (
	AgentStatusOffline AgentStatus = 0
	AgentStatusOnline  AgentStatus = 1
	AgentStatusError   AgentStatus = 2
)

type Agent struct {
	Base
	Name         crypto.EncryptedString `gorm:"type:text;not null"`
	KeyId        crypto.EncryptedString `gorm:"type:text;not null;"`
	Status       AgentStatus            `gorm:"type:integer;default:0"`
	LastSeen     *time.Time
	Applications []Application `gorm:"many2many:application_agents;constraint:OnDelete:CASCADE;"`
}
