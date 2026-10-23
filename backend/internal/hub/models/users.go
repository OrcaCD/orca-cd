package models

type UserRole string

const (
	UserRoleAdmin UserRole = "admin"
	UserRoleUser  UserRole = "user"
)

type User struct {
	Base
	Email                  string   `gorm:"type:text;uniqueIndex;not null"`
	Name                   string   `gorm:"type:text;not null"`
	PasswordHash           *string  `gorm:"type:text;"`
	PasswordChangeRequired bool     `gorm:"type:integer;not null"`
	Role                   UserRole `gorm:"type:text;not null;default:'user'"`
	TokenVersion           int64    `gorm:"type:integer;not null;default:0"`
	// EmailUnconfirmed is set when users change their own email. Such an email
	// is not trusted for linking an OIDC login to the account.
	EmailUnconfirmed bool `gorm:"type:integer;not null;default:0"`
}
