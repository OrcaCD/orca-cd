package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
)

// setupImportTestEnv changes the working directory to an isolated temp dir and
// sets the minimum env vars required by hub.DefaultConfig().
func setupImportTestEnv(t *testing.T) {
	t.Helper()

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	workDir := t.TempDir()
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("failed to change working directory: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workDir, "data"), 0750); err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		_ = os.Chdir(originalWD)
	})

	t.Setenv("APP_URL", "http://localhost")
	t.Setenv("APP_SECRET", "test-secret-that-is-long-enough-32ch")
}

func TestRunImportCommand_Succeeds(t *testing.T) {
	setupImportTestEnv(t)

	// First create a backup to import
	var backupOut bytes.Buffer
	if err := runBackupCommand(&backupOut, "test-backup.db"); err != nil {
		t.Fatalf("runBackupCommand() unexpected error: %v", err)
	}

	backupPath := filepath.Join("data", "test-backup.db")
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("expected backup file at %q: %v", backupPath, err)
	}

	// Now delete the current database and import the backup
	if err := os.Remove(filepath.Join("data", "hub.db")); err != nil {
		t.Fatalf("failed to delete current database: %v", err)
	}

	var importOut bytes.Buffer
	if err := runImportCommand(&importOut, backupPath); err != nil {
		t.Fatalf("runImportCommand() unexpected error: %v", err)
	}

	// Check that the database file exists
	if _, err := os.Stat(filepath.Join("data", "hub.db")); err != nil {
		t.Errorf("expected database file to exist after import: %v", err)
	}

	if !strings.Contains(importOut.String(), "Import Successful") {
		t.Errorf("expected output to contain %q, got: %q", "Import Successful", importOut.String())
	}
}

func TestRunImportCommand_OutputContainsSourcePath(t *testing.T) {
	setupImportTestEnv(t)

	// Create a backup to import
	var backupOut bytes.Buffer
	if err := runBackupCommand(&backupOut, "test-backup.db"); err != nil {
		t.Fatalf("runBackupCommand() unexpected error: %v", err)
	}

	backupPath := filepath.Join("data", "test-backup.db")

	// Delete the current database and import
	if err := os.Remove(filepath.Join("data", "hub.db")); err != nil {
		t.Fatalf("failed to delete current database: %v", err)
	}

	var importOut bytes.Buffer
	if err := runImportCommand(&importOut, backupPath); err != nil {
		t.Fatalf("runImportCommand() unexpected error: %v", err)
	}

	if !strings.Contains(importOut.String(), backupPath) {
		t.Errorf("expected output to contain source path %q, got: %q", backupPath, importOut.String())
	}
}

func TestRunImportCommand_FailsIfFileNotFound(t *testing.T) {
	setupImportTestEnv(t)

	var out bytes.Buffer
	err := runImportCommand(&out, "nonexistent-backup.db")
	if err == nil {
		t.Fatal("runImportCommand() expected error for nonexistent file, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected error to mention 'not found', got: %v", err)
	}
}

func TestRunImportCommand_FailsInDemoMode(t *testing.T) {
	setupImportTestEnv(t)
	t.Setenv("DEMO", "true")

	// Create a backup file
	backupPath := filepath.Join("data", "test-backup.db")
	if err := os.WriteFile(backupPath, []byte{}, 0600); err != nil {
		t.Fatalf("failed to create test backup file: %v", err)
	}

	var out bytes.Buffer
	err := runImportCommand(&out, backupPath)
	if err == nil {
		t.Fatal("runImportCommand() expected error in demo mode, got nil")
	}
	if !strings.Contains(err.Error(), "demo mode") {
		t.Errorf("expected error to mention 'demo mode', got: %v", err)
	}
}

func TestRunImportCommand_AbsoluteBackupPath(t *testing.T) {
	setupImportTestEnv(t)

	// Create a backup at an absolute path
	var backupOut bytes.Buffer
	absBackupPath := filepath.Join(t.TempDir(), "absolute-backup.db")
	if err := runBackupCommand(&backupOut, absBackupPath); err != nil {
		t.Fatalf("runBackupCommand() unexpected error: %v", err)
	}

	// Delete the current database and import from absolute path
	if err := os.Remove(filepath.Join("data", "hub.db")); err != nil {
		t.Fatalf("failed to delete current database: %v", err)
	}

	var importOut bytes.Buffer
	if err := runImportCommand(&importOut, absBackupPath); err != nil {
		t.Fatalf("runImportCommand() unexpected error with absolute path: %v", err)
	}

	if !strings.Contains(importOut.String(), "Import Successful") {
		t.Errorf("expected output to contain success message, got: %q", importOut.String())
	}
}
