package models

import (
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
)

type OIDCProvider struct {
	Base
	Name                 string                 `gorm:"column:name;type:text;not null"`
	IssuerURL            string                 `gorm:"column:issuer_url;type:text;not null"`
	ClientId             string                 `gorm:"column:client_id;type:text;not null"`
	ClientSecret         crypto.EncryptedString `gorm:"column:client_secret;type:text;not null"`
	Scopes               string                 `gorm:"column:scopes;type:text;not null;default:''"`
	Enabled              bool                   `gorm:"column:enabled;type:integer;not null;default:1"`
	RequireVerifiedEmail bool                   `gorm:"column:require_verified_email;type:integer;not null"`
	AutoSignup           bool                   `gorm:"column:auto_signup;type:integer;not null"`
}

func (OIDCProvider) TableName() string {
	return "oidc_providers"
}
