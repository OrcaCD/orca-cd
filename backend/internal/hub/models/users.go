package models

type AuthProvider string

const (
	AuthProviderLocal AuthProvider = "local"
)

type User struct {
	Base
	Username     string       `gorm:"type:text;uniqueIndex;not null"`
	PasswordHash string       `gorm:"type:text;not null;default:''"`
	AuthProvider AuthProvider `gorm:"type:text;not null;default:'local'"`
}
