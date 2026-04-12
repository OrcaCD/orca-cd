package db

import (
	"errors"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	demoSeedUserID         = "demo-user"
	demoSeedUserEmail      = "demo@orcacd.dev"
	demoSeedUserPassword   = "demo-password"
	demoSeedUserName       = "Demo User"
	demoSeedAgentID        = "demo-agent"
	demoSeedAgentName      = "Demo Agent"
	demoSeedAgentKeyID     = "demo-agent-key"
	demoSeedRepositoryID   = "demo-repository"
	demoSeedRepositoryName = "OrcaCD/demo-app"
	demoSeedRepositoryURL  = "https://github.com/OrcaCD/demo-app"
)

func seedDemoData(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		hashedPassword, _ := auth.HashPassword(demoSeedUserPassword)

		user := models.User{
			Base:                   models.Base{Id: demoSeedUserID},
			Email:                  demoSeedUserEmail,
			Name:                   demoSeedUserName,
			Role:                   models.UserRoleAdmin,
			PasswordChangeRequired: false,
			PasswordHash:           &hashedPassword,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Select("*").Create(&user).Error; err != nil {
			return err
		}

		var userIDs []string
		if err := tx.Model(&models.User{}).Where("email = ?", demoSeedUserEmail).Limit(1).Pluck("id", &userIDs).Error; err != nil {
			return err
		}
		if len(userIDs) == 0 {
			return errors.New("failed to resolve demo user after seeding")
		}

		agent := models.Agent{
			Base:   models.Base{Id: demoSeedAgentID},
			Name:   crypto.EncryptedString(demoSeedAgentName),
			KeyId:  crypto.EncryptedString(demoSeedAgentKeyID),
			Status: models.AgentStatusOffline,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Select("*").Create(&agent).Error; err != nil {
			return err
		}

		repository := models.Repository{
			Base:       models.Base{Id: demoSeedRepositoryID},
			Name:       demoSeedRepositoryName,
			Url:        demoSeedRepositoryURL,
			Provider:   models.GitHub,
			AuthMethod: models.AuthMethodNone,
			SyncType:   models.SyncTypeManual,
			SyncStatus: models.SyncStatusUnknown,
			CreatedBy:  userIDs[0],
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Select("*").Create(&repository).Error; err != nil {
			return err
		}

		return nil
	})
}
