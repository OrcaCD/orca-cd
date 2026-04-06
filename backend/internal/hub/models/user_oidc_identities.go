package models

type UserOIDCIdentity struct {
	Base
	UserId     string `gorm:"column:user_id;type:text;not null;index;uniqueIndex:idx_user_oidc_identities_user_provider,priority:1"`
	ProviderId string `gorm:"column:provider_id;type:text;not null;index;uniqueIndex:idx_user_oidc_identities_user_provider,priority:2;uniqueIndex:idx_user_oidc_identities_provider_subject,priority:1"`
	Subject    string `gorm:"column:subject;type:text;not null;uniqueIndex:idx_user_oidc_identities_provider_subject,priority:2"`
}

func (UserOIDCIdentity) TableName() string {
	return "user_oidc_identities"
}
