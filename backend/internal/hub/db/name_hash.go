package db

import (
	"context"
	"errors"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"gorm.io/gorm"
)

// isUniqueConstraintError reports whether err is a unique-constraint violation.
// Mirrors the routes-package helper, duplicated here because routes imports db.
func isUniqueConstraintError(err error) bool {
	return errors.Is(err, gorm.ErrDuplicatedKey) ||
		strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}

// BackfillNameHashes recomputes the blind-index name_hash for every application
// using the current default cipher. It is idempotent and is run on startup (to
// populate rows created before the column existed) and after key rotation (when
// the blind-index key derived from APP_SECRET has changed).
func BackfillNameHashes(ctx context.Context, gdb *gorm.DB) error {
	apps, err := gorm.G[models.Application](gdb).Find(ctx)
	if err != nil {
		return err
	}

	for i := range apps {
		hash := crypto.BlindIndex(models.NormalizeName(apps[i].Name.String()))
		if hash == apps[i].NameHash {
			continue
		}
		if _, err := gorm.G[models.Application](gdb).
			Where("id = ?", apps[i].Id).
			Select("NameHash").
			Updates(ctx, models.Application{NameHash: hash}); err != nil {
			if isUniqueConstraintError(err) {
				logger.Warn().Str("applicationId", apps[i].Id).
					Msg("skipping name_hash backfill: duplicate application name on the same agent; rename one to enforce uniqueness")
				continue
			}
			return err
		}
	}

	return nil
}
