package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRestore_FailsWhenDBNil(t *testing.T) {
	originalDB := DB
	DB = nil
	t.Cleanup(func() { DB = originalDB })

	backupPath := filepath.Join(t.TempDir(), "test-backup.db")
	if err := Restore(backupPath); err == nil {
		t.Fatal("Restore() expected error when DB is nil, got nil")
	}
}

func TestRestore_FailsWhenBackupNotFound(t *testing.T) {
	setupGlobalDB(t)

	backupPath := filepath.Join(t.TempDir(), "nonexistent-backup.db")
	if err := Restore(backupPath); err == nil {
		t.Fatal("Restore() expected error when backup file doesn't exist, got nil")
	}
}

func TestCopyFile_Succeeds(t *testing.T) {
	src := filepath.Join(t.TempDir(), "source.db")
	dst := filepath.Join(t.TempDir(), "dest.db")

	srcContent := []byte("test database content")
	if err := os.WriteFile(src, srcContent, 0600); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile() unexpected error: %v", err)
	}

	dstContent, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("failed to read destination file: %v", err)
	}

	if string(dstContent) != string(srcContent) {
		t.Errorf("copyFile() content mismatch: got %q, want %q", string(dstContent), string(srcContent))
	}
}

func TestCopyFile_FailsWhenSourceNotFound(t *testing.T) {
	src := filepath.Join(t.TempDir(), "nonexistent.db")
	dst := filepath.Join(t.TempDir(), "dest.db")

	if err := copyFile(src, dst); err == nil {
		t.Fatal("copyFile() expected error when source doesn't exist, got nil")
	}
}

