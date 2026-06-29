package applications

import (
	"context"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"gorm.io/gorm"
)

// BackfillNameHashes recomputes the blind-index name_hash for every application
// using the current default cipher. It is idempotent and is run on startup (to
// populate rows created before the column existed) and after key rotation (when
// the blind-index key derived from APP_SECRET has changed).
func BackfillNameHashes(ctx context.Context) error {
	apps, err := gorm.G[models.Application](db.DB).Find(ctx)
	if err != nil {
		return err
	}

	for i := range apps {
		hash := crypto.BlindIndex(models.NormalizeName(apps[i].Name.String()))
		if hash == apps[i].NameHash {
			continue
		}
		if _, err := gorm.G[models.Application](db.DB).
			Where("id = ?", apps[i].Id).
			Select("NameHash").
			Updates(ctx, models.Application{NameHash: hash}); err != nil {
			return err
		}
	}

	return nil
}
