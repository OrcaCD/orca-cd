package db

import (
	"context"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"gorm.io/gorm"
)

// newTestApp builds a valid Application with all not-null columns populated. The
// caller sets Name and NameHash to model the row state under test.
func newTestApp(name, nameHash string) *models.Application {
	return &models.Application{
		Name:                crypto.EncryptedString(name),
		NameHash:            nameHash,
		RepositoryId:        "repo-1",
		AgentId:             "agent-1",
		SyncStatus:          models.UnknownSync,
		HealthStatus:        models.UnknownHealth,
		Branch:              "main",
		Commit:              "abc123",
		CommitMessage:       "init",
		Path:                "/",
		ComposeFile:         "services: {}",
		PreviousComposeFile: "services: {}",
	}
}

// setupNameHashTest returns a migrated DB with crypto initialized, ready for
// creating Application rows.
func setupNameHashTest(t *testing.T) *gorm.DB {
	t.Helper()
	initTestCrypto(t)
	gormDB := openTestDB(t)
	if err := runMigrations(gormDB); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}
	return gormDB
}

func fetchApp(t *testing.T, gormDB *gorm.DB, id string) models.Application {
	t.Helper()
	var app models.Application
	if err := gormDB.Where("id = ?", id).First(&app).Error; err != nil {
		t.Fatalf("failed to fetch application %q: %v", id, err)
	}
	return app
}

// TestBackfillNameHashes_PopulatesEmptyHashes covers the primary startup case:
// rows created before the name_hash column existed (empty NameHash) get a
// correct blind index computed and stored.
func TestBackfillNameHashes_PopulatesEmptyHashes(t *testing.T) {
	gormDB := setupNameHashTest(t)

	app := newTestApp("My App", "")
	if err := gormDB.Create(app).Error; err != nil {
		t.Fatalf("failed to create application: %v", err)
	}

	if err := BackfillNameHashes(context.Background(), gormDB); err != nil {
		t.Fatalf("BackfillNameHashes() error: %v", err)
	}

	want := crypto.BlindIndex(models.NormalizeName("My App"))
	got := fetchApp(t, gormDB, app.Id)
	if got.NameHash != want {
		t.Errorf("NameHash = %q, want %q", got.NameHash, want)
	}
	if got.NameHash == "" {
		t.Error("NameHash should not be empty after backfill")
	}
}

// TestBackfillNameHashes_NormalizesName verifies the stored hash is derived from
// the normalized (lower-cased, trimmed) name, matching lookups done by handlers.
func TestBackfillNameHashes_NormalizesName(t *testing.T) {
	gormDB := setupNameHashTest(t)

	app := newTestApp("  MixedCase App  ", "")
	if err := gormDB.Create(app).Error; err != nil {
		t.Fatalf("failed to create application: %v", err)
	}

	if err := BackfillNameHashes(context.Background(), gormDB); err != nil {
		t.Fatalf("BackfillNameHashes() error: %v", err)
	}

	want := crypto.BlindIndex("mixedcase app")
	got := fetchApp(t, gormDB, app.Id)
	if got.NameHash != want {
		t.Errorf("NameHash = %q, want %q (normalized)", got.NameHash, want)
	}
}

// TestBackfillNameHashes_CorrectsStaleHash covers the key-rotation case: a row
// with a wrong/stale hash gets overwritten with the correct one.
func TestBackfillNameHashes_CorrectsStaleHash(t *testing.T) {
	gormDB := setupNameHashTest(t)

	app := newTestApp("Rotated App", "stale-hash-from-old-key")
	if err := gormDB.Create(app).Error; err != nil {
		t.Fatalf("failed to create application: %v", err)
	}

	if err := BackfillNameHashes(context.Background(), gormDB); err != nil {
		t.Fatalf("BackfillNameHashes() error: %v", err)
	}

	want := crypto.BlindIndex(models.NormalizeName("Rotated App"))
	got := fetchApp(t, gormDB, app.Id)
	if got.NameHash != want {
		t.Errorf("NameHash = %q, want %q", got.NameHash, want)
	}
}

// TestBackfillNameHashes_SkipsCorrectHash covers the `continue` branch: rows that
// already hold the correct hash are not re-written (UpdatedAt stays unchanged).
func TestBackfillNameHashes_SkipsCorrectHash(t *testing.T) {
	gormDB := setupNameHashTest(t)

	correct := crypto.BlindIndex(models.NormalizeName("Stable App"))
	app := newTestApp("Stable App", correct)
	if err := gormDB.Create(app).Error; err != nil {
		t.Fatalf("failed to create application: %v", err)
	}
	before := fetchApp(t, gormDB, app.Id)

	if err := BackfillNameHashes(context.Background(), gormDB); err != nil {
		t.Fatalf("BackfillNameHashes() error: %v", err)
	}

	after := fetchApp(t, gormDB, app.Id)
	if after.NameHash != correct {
		t.Errorf("NameHash = %q, want %q", after.NameHash, correct)
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("UpdatedAt changed (%v -> %v); row should have been skipped", before.UpdatedAt, after.UpdatedAt)
	}
}

// TestBackfillNameHashes_MultipleRows verifies mixed row states are all handled
// in a single pass.
func TestBackfillNameHashes_MultipleRows(t *testing.T) {
	gormDB := setupNameHashTest(t)

	empty := newTestApp("App One", "")
	stale := newTestApp("App Two", "wrong")
	correct := newTestApp("App Three", crypto.BlindIndex(models.NormalizeName("App Three")))
	for _, app := range []*models.Application{empty, stale, correct} {
		if err := gormDB.Create(app).Error; err != nil {
			t.Fatalf("failed to create application: %v", err)
		}
	}

	if err := BackfillNameHashes(context.Background(), gormDB); err != nil {
		t.Fatalf("BackfillNameHashes() error: %v", err)
	}

	cases := map[string]string{
		empty.Id:   crypto.BlindIndex(models.NormalizeName("App One")),
		stale.Id:   crypto.BlindIndex(models.NormalizeName("App Two")),
		correct.Id: crypto.BlindIndex(models.NormalizeName("App Three")),
	}
	for id, want := range cases {
		if got := fetchApp(t, gormDB, id); got.NameHash != want {
			t.Errorf("app %q NameHash = %q, want %q", id, got.NameHash, want)
		}
	}
}

// TestBackfillNameHashes_EmptyTable verifies the no-rows case returns nil.
func TestBackfillNameHashes_EmptyTable(t *testing.T) {
	gormDB := setupNameHashTest(t)

	if err := BackfillNameHashes(context.Background(), gormDB); err != nil {
		t.Fatalf("BackfillNameHashes() on empty table error: %v", err)
	}
}

// TestBackfillNameHashes_Idempotent verifies a second run is a no-op and leaves
// hashes intact.
func TestBackfillNameHashes_Idempotent(t *testing.T) {
	gormDB := setupNameHashTest(t)

	app := newTestApp("Idem App", "")
	if err := gormDB.Create(app).Error; err != nil {
		t.Fatalf("failed to create application: %v", err)
	}

	if err := BackfillNameHashes(context.Background(), gormDB); err != nil {
		t.Fatalf("first BackfillNameHashes() error: %v", err)
	}
	first := fetchApp(t, gormDB, app.Id)

	if err := BackfillNameHashes(context.Background(), gormDB); err != nil {
		t.Fatalf("second BackfillNameHashes() error: %v", err)
	}
	second := fetchApp(t, gormDB, app.Id)

	if first.NameHash != second.NameHash {
		t.Errorf("hash changed between runs: %q -> %q", first.NameHash, second.NameHash)
	}
	if !first.UpdatedAt.Equal(second.UpdatedAt) {
		t.Errorf("UpdatedAt changed on second run (%v -> %v); should be a no-op", first.UpdatedAt, second.UpdatedAt)
	}
}

// TestBackfillNameHashes_FindError covers the error path from the initial Find
// query by running against a closed DB connection.
func TestBackfillNameHashes_FindError(t *testing.T) {
	gormDB := setupNameHashTest(t)

	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("failed to close sql.DB: %v", err)
	}

	if err := BackfillNameHashes(context.Background(), gormDB); err == nil {
		t.Fatal("BackfillNameHashes() expected error on closed DB, got nil")
	}
}

// TestBackfillNameHashes_UpdateError covers the Updates error path: a row needs
// updating (empty hash) but the DB is switched to read-only, so Find succeeds
// while the write fails.
func TestBackfillNameHashes_UpdateError(t *testing.T) {
	gormDB := setupNameHashTest(t)

	app := newTestApp("RO App", "")
	if err := gormDB.Create(app).Error; err != nil {
		t.Fatalf("failed to create application: %v", err)
	}

	if err := gormDB.Exec("PRAGMA query_only = ON").Error; err != nil {
		t.Fatalf("failed to set query_only: %v", err)
	}

	if err := BackfillNameHashes(context.Background(), gormDB); err == nil {
		t.Fatal("BackfillNameHashes() expected error on read-only DB, got nil")
	}
}
