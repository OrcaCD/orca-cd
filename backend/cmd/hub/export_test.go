package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
)

func TestResolveExportPath_RelativePath(t *testing.T) {
	got := resolveExportPath("export.db")
	want := filepath.Join("data", "export.db")
	if got != want {
		t.Errorf("resolveExportPath(%q) = %q, want %q", "export.db", got, want)
	}
}

func TestResolveExportPath_AbsolutePath(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "export.db")
	if got := resolveExportPath(abs); got != abs {
		t.Errorf("resolveExportPath(%q) = %q, want %q", abs, got, abs)
	}
}

func TestResolveExportPath_RelativeSubdir(t *testing.T) {
	got := resolveExportPath("subdir/export.db")
	want := filepath.Join("data", "subdir", "export.db")
	if got != want {
		t.Errorf("resolveExportPath(%q) = %q, want %q", "subdir/export.db", got, want)
	}
}

// setupExportTestEnv changes the working directory to an isolated temp dir and
// sets the minimum env vars required by hub.DefaultConfig().
func setupExportTestEnv(t *testing.T) {
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

func TestRunExportCommand_Succeeds(t *testing.T) {
	setupExportTestEnv(t)

	var out bytes.Buffer
	if err := runExportCommand(&out, "export.db"); err != nil {
		t.Fatalf("runExportCommand() unexpected error: %v", err)
	}

	exportPath := filepath.Join("data", "export.db")
	if _, err := os.Stat(exportPath); err != nil {
		t.Errorf("expected export file at %q: %v", exportPath, err)
	}

	if !strings.Contains(out.String(), "Export Successful") {
		t.Errorf("expected output to contain %q, got: %q", "Export Successful", out.String())
	}
}

func TestRunExportCommand_OutputContainsResolvedPath(t *testing.T) {
	setupExportTestEnv(t)

	var out bytes.Buffer
	if err := runExportCommand(&out, "my-export.db"); err != nil {
		t.Fatalf("runExportCommand() unexpected error: %v", err)
	}

	resolvedPath := filepath.Join("data", "my-export.db")
	if !strings.Contains(out.String(), resolvedPath) {
		t.Errorf("expected output to contain resolved path %q, got: %q", resolvedPath, out.String())
	}
}

func TestRunExportCommand_FailsIfOutputExists(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(dataDir, "export.db"), []byte{}, 0600); err != nil {
		t.Fatalf("failed to create existing export file: %v", err)
	}

	var out bytes.Buffer
	err := runExportCommand(&out, "export.db")
	if err == nil {
		t.Fatal("runExportCommand() expected error for existing output file, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected error to mention 'already exists', got: %v", err)
	}
}

func TestRunExportCommand_FailsInDemoMode(t *testing.T) {
	setupExportTestEnv(t)
	t.Setenv("DEMO", "true")

	var out bytes.Buffer
	err := runExportCommand(&out, "export.db")
	if err == nil {
		t.Fatal("runExportCommand() expected error in demo mode, got nil")
	}
	if !strings.Contains(err.Error(), "demo mode") {
		t.Errorf("expected error to mention 'demo mode', got: %v", err)
	}
}

func TestRunExportCommand_AbsoluteOutputPath(t *testing.T) {
	setupExportTestEnv(t)

	absOutput := filepath.Join(t.TempDir(), "absolute-export.db")
	var out bytes.Buffer
	if err := runExportCommand(&out, absOutput); err != nil {
		t.Fatalf("runExportCommand() unexpected error with absolute path: %v", err)
	}

	if _, err := os.Stat(absOutput); err != nil {
		t.Errorf("expected export file at absolute path %q: %v", absOutput, err)
	}
}
