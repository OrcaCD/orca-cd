// Package releaseage resolves the minimum release age of an application from
// its own settings and the global defaults.
package releaseage

import (
	"context"
	"errors"
	"slices"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"gorm.io/gorm"
)

// Global returns the global settings. Without a database or a row, e.g. in
// tests that don't run the migrations, the release age is disabled.
func Global(ctx context.Context) (models.Settings, error) {
	if db.DB == nil {
		return models.Settings{Id: models.SettingsId}, nil
	}
	settings, err := gorm.G[models.Settings](db.DB).Where("id = ?", models.SettingsId).First(ctx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.Settings{Id: models.SettingsId}, nil
	}
	return settings, err
}

// MinimumSeconds returns the minimum release age of the application, and
// whether it uses the global default.
func MinimumSeconds(app *models.Application, global models.Settings) (seconds int64, inherited bool) {
	if app.MinimumReleaseAgeSeconds == nil {
		return global.MinimumReleaseAgeSeconds, true
	}
	return *app.MinimumReleaseAgeSeconds, false
}

// AllowedRegistries returns the global registries and those of the application.
func AllowedRegistries(app *models.Application, global models.Settings) []string {
	registries := slices.Clone(global.ReleaseAgeAllowedRegistries)
	for _, registry := range app.ReleaseAgeAllowedRegistries {
		if !slices.Contains(registries, registry) {
			registries = append(registries, registry)
		}
	}
	return registries
}

// Policy returns the release age policy of the application for its agent.
func Policy(app *models.Application, global models.Settings) *messages.ReleaseAgePolicy {
	seconds, _ := MinimumSeconds(app, global)
	return &messages.ReleaseAgePolicy{
		MinimumSeconds:        seconds,
		AllowedRegistries:     AllowedRegistries(app, global),
		ServiceMinimumSeconds: app.ReleaseAgeServiceOverrides,
	}
}
