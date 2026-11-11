package models

// SettingsId is the id of the single row of the settings table.
const SettingsId = 1

// Settings are the global settings of the hub.
type Settings struct {
	Id int `gorm:"primaryKey"`
	// MinimumReleaseAgeSeconds is the default minimum release age of
	// applications that don't set their own.
	MinimumReleaseAgeSeconds int64 `gorm:"not null;default:0"`
	// ReleaseAgeAllowedRegistries skip the release age check for every application.
	ReleaseAgeAllowedRegistries []string `gorm:"serializer:json;type:text;not null;default:'[]'"`
}

func (Settings) TableName() string { return "settings" }
