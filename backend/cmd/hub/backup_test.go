package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
)

func TestResolveBackupPath_RelativePath(t *testing.T) {
	got := resolveBackupPath("backup.db")
	want := filepath.Join("data", "backup.db")
	if got != want {
		t.Errorf("resolveBackupPath(%q) = %q, want %q", "backup.db", got, want)
	}
}

func TestResolveBackupPath_AbsolutePath(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "backup.db")
	if got := resolveBackupPath(abs); got != abs {
		t.Errorf("resolveBackupPath(%q) = %q, want %q", abs, got, abs)
	}
}

func TestResolveBackupPath_RelativeSubdir(t *testing.T) {
	got := resolveBackupPath("subdir/backup.db")
	want := filepath.Join("data", "subdir", "backup.db")
	if got != want {
		t.Errorf("resolveBackupPath(%q) = %q, want %q", "subdir/backup.db", got, want)
	}
}

// setupBackupTestEnv changes the working directory to an isolated temp dir and
// sets the minimum env vars required by hub.DefaultConfig().
func setupBackupTestEnv(t *testing.T) {
	t.Helper()

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	workDir := t.TempDir()
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("failed to change working directory: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		_ = os.Chdir(originalWD)
	})

	t.Setenv("APP_URL", "http://localhost")
	t.Setenv("APP_SECRET", "test-secret-that-is-long-enough-32ch")
}

func TestRunBackupCommand_Succeeds(t *testing.T) {
	setupBackupTestEnv(t)

	var out bytes.Buffer
	if err := runBackupCommand(&out, "backup.db"); err != nil {
		t.Fatalf("runBackupCommand() unexpected error: %v", err)
	}

	backupPath := filepath.Join("data", "backup.db")
	if _, err := os.Stat(backupPath); err != nil {
		t.Errorf("expected backup file at %q: %v", backupPath, err)
	}

	if !strings.Contains(out.String(), "Backup Successful") {
		t.Errorf("expected output to contain %q, got: %q", "Backup Successful", out.String())
	}
}

func TestRunBackupCommand_OutputContainsResolvedPath(t *testing.T) {
	setupBackupTestEnv(t)

	var out bytes.Buffer
	if err := runBackupCommand(&out, "my-backup.db"); err != nil {
		t.Fatalf("runBackupCommand() unexpected error: %v", err)
	}

	resolvedPath := filepath.Join("data", "my-backup.db")
	if !strings.Contains(out.String(), resolvedPath) {
		t.Errorf("expected output to contain resolved path %q, got: %q", resolvedPath, out.String())
	}
}

func TestRunBackupCommand_FailsIfOutputExists(t *testing.T) {
	originalWD, _ := os.Getwd()
	workDir := t.TempDir()
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("failed to change working directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWD) })

	dataDir := filepath.Join(workDir, "data")
	if err := os.MkdirAll(dataDir, 0750); err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "backup.db"), []byte{}, 0600); err != nil {
		t.Fatalf("failed to create existing backup file: %v", err)
	}

	var out bytes.Buffer
	err := runBackupCommand(&out, "backup.db")
	if err == nil {
		t.Fatal("runBackupCommand() expected error for existing output file, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected error to mention 'already exists', got: %v", err)
	}
}

func TestRunBackupCommand_FailsInDemoMode(t *testing.T) {
	setupBackupTestEnv(t)
	t.Setenv("DEMO", "true")

	var out bytes.Buffer
	err := runBackupCommand(&out, "backup.db")
	if err == nil {
		t.Fatal("runBackupCommand() expected error in demo mode, got nil")
	}
	if !strings.Contains(err.Error(), "demo mode") {
		t.Errorf("expected error to mention 'demo mode', got: %v", err)
	}
}

func TestRunBackupCommand_AbsoluteOutputPath(t *testing.T) {
	setupBackupTestEnv(t)

	absOutput := filepath.Join(t.TempDir(), "absolute-backup.db")
	var out bytes.Buffer
	if err := runBackupCommand(&out, absOutput); err != nil {
		t.Fatalf("runBackupCommand() unexpected error with absolute path: %v", err)
	}

	if _, err := os.Stat(absOutput); err != nil {
		t.Errorf("expected backup file at absolute path %q: %v", absOutput, err)
	}
}
