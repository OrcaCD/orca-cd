package db

import (
	"context"
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
	ctx := context.Background()

	userCount, err := gorm.G[models.User](db).Count(ctx, "*")
	if err != nil {
		return err
	}

	// Assume seed already done
	if userCount > 1 {
		return nil
	}

	return db.Transaction(func(tx *gorm.DB) error {
		hashedPassword, err := auth.HashPassword(demoSeedUserPassword)
		if err != nil {
			return err
		}

		user := models.User{
			Base:                   models.Base{Id: demoSeedUserID},
			Email:                  demoSeedUserEmail,
			Name:                   demoSeedUserName,
			Role:                   models.UserRoleAdmin,
			PasswordChangeRequired: false,
			PasswordHash:           &hashedPassword,
		}
		if err := gorm.G[models.User](tx, clause.OnConflict{DoNothing: true}).Create(ctx, &user); err != nil {
			return err
		}

		resolvedUser, err := gorm.G[models.User](tx).Where("email = ?", demoSeedUserEmail).First(ctx)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("failed to resolve demo user after seeding")
			}
			return err
		}

		agent := models.Agent{
			Base:   models.Base{Id: demoSeedAgentID},
			Name:   crypto.EncryptedString(demoSeedAgentName),
			KeyId:  crypto.EncryptedString(demoSeedAgentKeyID),
			Status: models.AgentStatusOffline,
		}
		if err := gorm.G[models.Agent](tx, clause.OnConflict{DoNothing: true}).Create(ctx, &agent); err != nil {
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
			CreatedBy:  resolvedUser.Id,
		}
		if err := gorm.G[models.Repository](tx, clause.OnConflict{DoNothing: true}).Create(ctx, &repository); err != nil {
			return err
		}

		return nil
	})
}
