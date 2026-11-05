package models

import (
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
)

type ApplicationDeploymentStatus string

const (
	// DeploymentPending is a deployment that was dispatched to the agent and
	// has not reported a result yet.
	DeploymentPending ApplicationDeploymentStatus = "pending"
	// DeploymentVerifying is a deployment that runs but is rolled back if it
	// becomes unhealthy before VerifyUntil.
	DeploymentVerifying ApplicationDeploymentStatus = "verifying"
	// DeploymentSucceeded is a deployment the application can be rolled back to.
	DeploymentSucceeded ApplicationDeploymentStatus = "succeeded"
)

// DeployedImage is the image a compose service ran in a deployment.
type DeployedImage struct {
	Service string `json:"service"`
	ImageId string `json:"imageId"`
	// PinnedRef is the registry reference pinned by digest, empty for images
	// that did not come from a registry.
	PinnedRef string `json:"pinnedRef,omitempty"`
}

// ApplicationDeployment is a snapshot of what an application ran: the compose
// file and the exact images of its services. It is what a rollback deploys.
type ApplicationDeployment struct {
	Base
	ApplicationId string                      `gorm:"type:text;not null;index:idx_application_deployments_app_deployed,priority:1"`
	Application   Application                 `gorm:"foreignKey:ApplicationId;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`
	RequestId     *string                     `gorm:"type:text;uniqueIndex"`
	Status        ApplicationDeploymentStatus `gorm:"type:text;not null"`
	Commit        string                      `gorm:"type:text;not null"`
	CommitMessage string                      `gorm:"type:text;not null"`
	ComposeFile   crypto.EncryptedString      `gorm:"type:text;not null"`
	Images        []DeployedImage             `gorm:"type:text;not null;serializer:json"`
	DeployedAt    *time.Time                  `gorm:"type:timestamp;index:idx_application_deployments_app_deployed,priority:2"`
	// PreviousDeploymentId is what the application ran before, which an
	// automatic rollback restores.
	PreviousDeploymentId *string    `gorm:"type:text"`
	VerifyUntil          *time.Time `gorm:"type:timestamp"`
}

func (ApplicationDeployment) TableName() string { return "application_deployments" }
