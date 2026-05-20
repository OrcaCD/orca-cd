package models

type AuditLog struct {
	Base
	UserId     *string `gorm:"type:text"`
	User       *User   `gorm:"foreignKey:UserId"`
	EventType  string  `gorm:"type:text;not null"`
	TargetType string  `gorm:"type:text;not null"`
	TargetId   *string `gorm:"type:text"`
}
