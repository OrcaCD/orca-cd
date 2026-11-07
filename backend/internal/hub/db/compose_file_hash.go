package db

import (
	"context"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"gorm.io/gorm"
)

// BackfillComposeFileHashes recomputes keyed compose-file blind indexes for
// existing applications. It also upgrades hashes stored before they were keyed
// and refreshes indexes after APP_SECRET rotation.
func BackfillComposeFileHashes(ctx context.Context, gdb *gorm.DB) error {
	apps, err := gorm.G[models.Application](gdb).
		Select("Id", "ComposeFile", "ComposeFileHash").
		Find(ctx)
	if err != nil {
		return err
	}

	for i := range apps {
		hash := crypto.BlindIndex(apps[i].ComposeFile.String())
		if hash == apps[i].ComposeFileHash {
			continue
		}
		if _, err := gorm.G[models.Application](gdb).
			Where("id = ? AND compose_file_hash = ?", apps[i].Id, apps[i].ComposeFileHash).
			Select("ComposeFileHash").
			Updates(ctx, models.Application{ComposeFileHash: hash}); err != nil {
			return err
		}
	}

	return nil
}
