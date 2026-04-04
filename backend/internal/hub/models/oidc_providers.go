package models

import (
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
)

type OIDCProvider struct {
	Base
	Name         string                 `gorm:"type:text;not null"`
	IssuerURL    string                 `gorm:"type:text;not null"`
	ClientID     string                 `gorm:"type:text;not null"`
	ClientSecret crypto.EncryptedString `gorm:"type:text;not null"`
	Scopes       string                 `gorm:"type:text;not null;default:''"`
	Enabled      bool                   `gorm:"type:integer;not null;default:1"`
}
