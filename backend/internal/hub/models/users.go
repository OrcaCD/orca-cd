package models

type AuthProvider string

const (
	AuthProviderLocal AuthProvider = "local"
	AuthProviderOIDC  AuthProvider = "oidc"
)

type UserRole string

const (
	UserRoleAdmin UserRole = "admin"
	UserRoleUser  UserRole = "user"
)

type User struct {
	Base
	Email        string       `gorm:"type:text;uniqueIndex;not null"`
	Name         string       `gorm:"type:text;not null"`
	PasswordHash *string      `gorm:"type:text;"`
	AuthProvider AuthProvider `gorm:"type:text;not null;default:'local'"`
	Role         UserRole     `gorm:"type:text;not null;default:'user'"`
	OIDCSubject  *string      `gorm:"column:oidc_subject;type:text;"`
	OIDCIssuer   *string      `gorm:"column:oidc_issuer;type:text;"`
}
