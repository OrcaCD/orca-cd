package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
)

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

func runBackupCommand(out *bytes.Buffer, backupPath string) error {
	return runExportCommand(out, backupPath)
}

func TestRunImportCommand_Succeeds(t *testing.T) {
	setupImportTestEnv(t)

	var backupOut bytes.Buffer
	if err := runBackupCommand(&backupOut, "test-backup.db"); err != nil {
		t.Fatalf("runBackupCommand() unexpected error: %v", err)
	}

	backupPath := filepath.Join("data", "test-backup.db")
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("expected backup file at %q: %v", backupPath, err)
	}

	if err := os.Remove(filepath.Join("data", "hub.db")); err != nil {
		t.Fatalf("failed to delete current database: %v", err)
	}

	var importOut bytes.Buffer
	if err := runImportCommandWithInput(&importOut, strings.NewReader("yes\n"), backupPath, true); err != nil {
		t.Fatalf("runImportCommandWithInput() unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join("data", "hub.db")); err != nil {
		t.Errorf("expected database file to exist after import: %v", err)
	}

	if !strings.Contains(importOut.String(), "Import Successful") {
		t.Errorf("expected output to contain %q, got: %q", "Import Successful", importOut.String())
	}
}

func TestRunImportCommand_OutputContainsSourcePath(t *testing.T) {
	setupImportTestEnv(t)

	var backupOut bytes.Buffer
	if err := runBackupCommand(&backupOut, "test-backup.db"); err != nil {
		t.Fatalf("runBackupCommand() unexpected error: %v", err)
	}

	backupPath := filepath.Join("data", "test-backup.db")

	if err := os.Remove(filepath.Join("data", "hub.db")); err != nil {
		t.Fatalf("failed to delete current database: %v", err)
	}

	var importOut bytes.Buffer
	if err := runImportCommandWithInput(&importOut, strings.NewReader("yes\n"), backupPath, true); err != nil {
		t.Fatalf("runImportCommandWithInput() unexpected error: %v", err)
	}

	if !strings.Contains(importOut.String(), backupPath) {
		t.Errorf("expected output to contain source path %q, got: %q", backupPath, importOut.String())
	}
}

func TestRunImportCommand_FailsIfFileNotFound(t *testing.T) {
	setupImportTestEnv(t)

	var out bytes.Buffer
	err := runImportCommand(&out, "nonexistent-backup.db", true)
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

	backupPath := filepath.Join("data", "test-backup.db")
	if err := os.WriteFile(backupPath, []byte{}, 0600); err != nil {
		t.Fatalf("failed to create test backup file: %v", err)
	}

	var out bytes.Buffer
	err := runImportCommandWithInput(&out, strings.NewReader("yes\n"), backupPath, true)
	if err == nil {
		t.Fatal("runImportCommandWithInput() expected error in demo mode, got nil")
	}
	if !strings.Contains(err.Error(), "demo mode") {
		t.Errorf("expected error to mention 'demo mode', got: %v", err)
	}
}

func TestRunImportCommand_AbsoluteBackupPath(t *testing.T) {
	setupImportTestEnv(t)

	var backupOut bytes.Buffer
	if err := runBackupCommand(&backupOut, "test-backup.db"); err != nil {
		t.Fatalf("runBackupCommand() unexpected error: %v", err)
	}

	relBackupPath := filepath.Join("data", "test-backup.db")
	absBackupPath, err := filepath.Abs(relBackupPath)
	if err != nil {
		t.Fatalf("failed to resolve absolute path: %v", err)
	}
	// Delete the current database and import from the absolute backup path.
	if err := os.Remove(filepath.Join("data", "hub.db")); err != nil {
		t.Fatalf("failed to delete current database: %v", err)
	}

	var importOut bytes.Buffer
	if err := runImportCommandWithInput(&importOut, strings.NewReader("yes\n"), absBackupPath, true); err != nil {
		t.Fatalf("runImportCommandWithInput() unexpected error: %v", err)
	}
}

func TestRunImportCommand_FailsIfBackupIsInvalid(t *testing.T) {
	setupImportTestEnv(t)

	backupPath := filepath.Join("data", "invalid-backup.db")
	if err := os.WriteFile(backupPath, []byte("invalid data"), 0600); err != nil {
		t.Fatalf("failed to create invalid backup file: %v", err)
	}

	if err := os.Remove(filepath.Join("data", "hub.db")); err != nil && !os.IsNotExist(err) {
		t.Fatalf("failed to delete current database: %v", err)
	}

	var importOut bytes.Buffer
	err := runImportCommandWithInput(&importOut, strings.NewReader("yes\n"), backupPath, true)
	if err != nil {
		if !strings.Contains(err.Error(), "import failed") && !strings.Contains(err.Error(), "restore") {
			t.Errorf("expected error related to import or restore, got: %v", err)
		}
	}
}

func TestRunImportCommand_MissingAppSecret(t *testing.T) {
	setupImportTestEnv(t)

	backupPath := filepath.Join("data", "test-backup.db")
	if err := os.WriteFile(backupPath, []byte("test"), 0600); err != nil {
		t.Fatalf("failed to create test backup file: %v", err)
	}

	t.Setenv("APP_SECRET", "")

	var importOut bytes.Buffer
	err := runImportCommandWithInput(&importOut, strings.NewReader("yes\n"), backupPath, true)
	if err == nil {
		t.Fatal("runImportCommandWithInput() expected error with missing APP_SECRET, got nil")
	}
	if !strings.Contains(err.Error(), "configuration") {
		t.Errorf("expected error to mention 'configuration', got: %v", err)
	}
}

func TestRunImportCommand_MissingAppURL(t *testing.T) {
	setupImportTestEnv(t)

	backupPath := filepath.Join("data", "test-backup.db")
	if err := os.WriteFile(backupPath, []byte("test"), 0600); err != nil {
		t.Fatalf("failed to create test backup file: %v", err)
	}

	t.Setenv("APP_URL", "")

	var importOut bytes.Buffer
	err := runImportCommandWithInput(&importOut, strings.NewReader("yes\n"), backupPath, true)
	if err == nil {
		t.Fatal("runImportCommandWithInput() expected error with missing APP_URL, got nil")
	}
	if !strings.Contains(err.Error(), "configuration") {
		t.Errorf("expected error to mention 'configuration', got: %v", err)
	}
}

func TestRunImportCommand_UserCancellation(t *testing.T) {
	setupImportTestEnv(t)

	backupPath := filepath.Join("data", "test-backup.db")
	if err := os.WriteFile(backupPath, []byte("test"), 0600); err != nil {
		t.Fatalf("failed to create test backup file: %v", err)
	}

	var importOut bytes.Buffer
	err := runImportCommandWithInput(&importOut, strings.NewReader("no\n"), backupPath, false)
	if err != nil {
		t.Fatalf("runImportCommandWithInput() unexpected error: %v", err)
	}

	if !strings.Contains(importOut.String(), "Import cancelled") {
		t.Errorf("expected output to contain 'Import cancelled', got: %q", importOut.String())
	}
}

func TestRunImportCommand_InvalidUserInput(t *testing.T) {
	setupImportTestEnv(t)

	backupPath := filepath.Join("data", "test-backup.db")
	if err := os.WriteFile(backupPath, []byte("test"), 0600); err != nil {
		t.Fatalf("failed to create test backup file: %v", err)
	}

	var importOut bytes.Buffer
	err := runImportCommandWithInput(&importOut, strings.NewReader("maybe\n"), backupPath, false)
	if err != nil {
		t.Fatalf("runImportCommandWithInput() unexpected error: %v", err)
	}

	if !strings.Contains(importOut.String(), "Import cancelled") {
		t.Errorf("expected output to contain 'Import cancelled', got: %q", importOut.String())
	}
}
