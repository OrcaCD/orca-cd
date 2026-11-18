package models

import (
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
)

type Passkey struct {
	Base
	UserId       string                 `gorm:"type:text;not null;index"`
	CredentialId string                 `gorm:"type:text;not null;uniqueIndex"`
	Credential   crypto.EncryptedString `gorm:"type:text;not null" json:"-"`
	Name         string                 `gorm:"type:text;not null"`
	LastUsedAt   *time.Time
}

// PasskeyEnrollment holds only the digest of a single-use enrollment code.
// One enrollment per user means issuing a replacement invalidates the old code.
type PasskeyEnrollment struct {
	UserId       string `gorm:"type:text;primaryKey"`
	TokenHash    string `gorm:"type:text;not null;uniqueIndex" json:"-"`
	TokenVersion int64  `gorm:"not null"`
	ExpiresAt    time.Time
}
