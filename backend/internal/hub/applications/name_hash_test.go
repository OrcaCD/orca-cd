package applications

import (
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"gorm.io/gorm"
)

func TestBackfillNameHashes_PopulatesExistingRows(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	agent := seedAgent(t)
	app := seedApp(t, repo.Id, agent.Id, "compose: v1")

	// seedApp leaves name_hash empty, mimicking a row created before the column existed.
	stored, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(t.Context())
	if err != nil {
		t.Fatalf("load seeded app: %v", err)
	}
	if stored.NameHash != "" {
		t.Fatalf("expected empty name_hash before backfill, got %q", stored.NameHash)
	}

	if err := BackfillNameHashes(t.Context()); err != nil {
		t.Fatalf("BackfillNameHashes: %v", err)
	}

	stored, err = gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(t.Context())
	if err != nil {
		t.Fatalf("reload app: %v", err)
	}
	want := crypto.BlindIndex(models.NormalizeName(stored.Name.String()))
	if stored.NameHash != want {
		t.Errorf("expected name_hash %q, got %q", want, stored.NameHash)
	}
}
