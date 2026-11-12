package models

import "time"

type ImageUpdateStatus string

const (
	ImageUpdateUpToDate ImageUpdateStatus = "up_to_date"
	// ImageUpdateWaiting is a new image that waits for the minimum release age.
	ImageUpdateWaiting ImageUpdateStatus = "waiting"
	// ImageUpdateBlocked is a new image held back until the configuration is fixed.
	ImageUpdateBlocked ImageUpdateStatus = "blocked"
	ImageUpdateUpdated ImageUpdateStatus = "updated"
	ImageUpdateFailed  ImageUpdateStatus = "failed"
)

// ApplicationImageUpdate is the update state of the image of one compose
// service, as of the last image check of the agent.
type ApplicationImageUpdate struct {
	ApplicationId  string            `gorm:"type:text;primaryKey"`
	Application    Application       `gorm:"foreignKey:ApplicationId;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`
	Service        string            `gorm:"type:text;primaryKey"`
	Image          string            `gorm:"type:text;not null"`
	Status         ImageUpdateStatus `gorm:"type:text;not null"`
	CurrentDigest  string            `gorm:"type:text;not null;default:''"`
	CurrentVersion string            `gorm:"type:text;not null;default:''"`
	NewDigest      string            `gorm:"type:text;not null;default:''"`
	NewVersion     string            `gorm:"type:text;not null;default:''"`
	PushedAt       *time.Time        `gorm:"type:timestamp"`
	// FirstSeen means PushedAt is when the agent first saw the new image, as
	// its registry has no push time.
	FirstSeen           bool       `gorm:"not null;default:false"`
	EligibleAt          *time.Time `gorm:"type:timestamp"`
	MinimumAgeSeconds   int64      `gorm:"not null;default:0"`
	MinimumAgeFromLabel bool       `gorm:"not null;default:false"`
	Reason              string     `gorm:"type:text;not null;default:''"`
	CheckedAt           time.Time  `gorm:"not null"`
}

func (ApplicationImageUpdate) TableName() string { return "application_image_updates" }
