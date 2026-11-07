package db

import (
	"context"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/shared/composewarnings"
)

func TestBackfillComposeFileHashesReplacesUnkeyedHash(t *testing.T) {
	gormDB := setupNameHashTest(t)

	app := newTestApp("Compose Hash App", "")
	app.ComposeFile = "services:\n  app:\n    image: example/app:1\n"
	app.ComposeFileHash = composewarnings.Hash(app.ComposeFile.String())
	if err := gormDB.Create(app).Error; err != nil {
		t.Fatalf("create application: %v", err)
	}

	if err := BackfillComposeFileHashes(context.Background(), gormDB); err != nil {
		t.Fatalf("BackfillComposeFileHashes() error: %v", err)
	}

	got := fetchApp(t, gormDB, app.Id)
	want := crypto.BlindIndex(app.ComposeFile.String())
	if got.ComposeFileHash != want {
		t.Errorf("ComposeFileHash = %q, want keyed hash %q", got.ComposeFileHash, want)
	}
	if got.ComposeFileHash == composewarnings.Hash(app.ComposeFile.String()) {
		t.Error("legacy unkeyed hash was not replaced")
	}
}

func TestBackfillComposeFileHashesIsIdempotent(t *testing.T) {
	gormDB := setupNameHashTest(t)

	app := newTestApp("Compose Hash App", "")
	app.ComposeFileHash = crypto.BlindIndex(app.ComposeFile.String())
	if err := gormDB.Create(app).Error; err != nil {
		t.Fatalf("create application: %v", err)
	}
	before := fetchApp(t, gormDB, app.Id)

	if err := BackfillComposeFileHashes(context.Background(), gormDB); err != nil {
		t.Fatalf("BackfillComposeFileHashes() error: %v", err)
	}

	after := fetchApp(t, gormDB, app.Id)
	if after.ComposeFileHash != before.ComposeFileHash {
		t.Errorf("ComposeFileHash changed: %q -> %q", before.ComposeFileHash, after.ComposeFileHash)
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("UpdatedAt changed on no-op backfill: %v -> %v", before.UpdatedAt, after.UpdatedAt)
	}
}

func TestBackfillComposeFileHashesFindError(t *testing.T) {
	gormDB := setupNameHashTest(t)
	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql.DB: %v", err)
	}

	if err := BackfillComposeFileHashes(context.Background(), gormDB); err == nil {
		t.Fatal("BackfillComposeFileHashes() expected error on closed DB")
	}
}

func TestBackfillComposeFileHashesUpdateError(t *testing.T) {
	gormDB := setupNameHashTest(t)

	app := newTestApp("Compose Hash App", "")
	app.ComposeFileHash = "legacy-unkeyed-sha256"
	if err := gormDB.Create(app).Error; err != nil {
		t.Fatalf("create application: %v", err)
	}
	if err := gormDB.Exec("PRAGMA query_only = ON").Error; err != nil {
		t.Fatalf("set query_only: %v", err)
	}

	if err := BackfillComposeFileHashes(context.Background(), gormDB); err == nil {
		t.Fatal("BackfillComposeFileHashes() expected update error on read-only DB")
	}
}
