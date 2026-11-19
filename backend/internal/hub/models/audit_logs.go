package models

type AuditLog struct {
	Base
	UserId     *string `gorm:"type:text"`
	User       *User   `gorm:"foreignKey:UserId"`
	EventType  string  `gorm:"type:text;not null"`
	TargetType string  `gorm:"type:text;not null"`
	TargetId   *string `gorm:"type:text"`
	// APITokenId is set when the action was made with an API token. It has no
	// foreign key, so it is kept after the token is revoked.
	APITokenId *string `gorm:"type:text"`
}
