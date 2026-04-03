package models

type AuthProvider string

const (
	AuthProviderLocal AuthProvider = "local"
)

type User struct {
	Base
	Email        string       `gorm:"type:text;uniqueIndex;not null"`
	Name         string       `gorm:"type:text;not null"`
	PasswordHash *string      `gorm:"type:text;"`
	AuthProvider AuthProvider `gorm:"type:text;not null;default:'local'"`
}
