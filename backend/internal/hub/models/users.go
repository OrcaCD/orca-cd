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
}
