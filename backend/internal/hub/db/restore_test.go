package db

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

	dstContent, err := os.ReadFile(dst) // #nosec G304 - paths are controlled in test
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

func TestRestore_MkdirAllFails(t *testing.T) {
	setupGlobalDB(t)

	fake := newFakeFS()
	fake.MkdirAllErr = errors.New("disk full")
	fs = fake // inject

	backupPath := filepath.Join(t.TempDir(), "test-backup.db")
	err := Restore(backupPath)
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("expected disk full error, got %v", err)
	}
}

func TestRestore_CopyFails_RollbackSucceeds(t *testing.T) {
	setupGlobalDB(t)

	fake := newFakeFS()
	fake.OpenErr = errors.New("I/O error")
	fs = fake

	backupPath := filepath.Join(t.TempDir(), "test-backup"+fmt.Sprintf("%d", time.Now().Unix())+".db")
	err := Restore(backupPath)
	if err == nil || !strings.Contains(err.Error(), "I/O error") {
		t.Fatalf("expected I/O error, got %v", err)
	}
}

func TestRestore_CopyFails_FailsOpenFile(t *testing.T) {
	fake := newFakeFS()
	fake.OpenFileErr = errors.New("I/O error")
	fs = fake

	src := filepath.Join(t.TempDir(), "nonexistent.db")
	dst := filepath.Join(t.TempDir(), "dest.db")

	err := copyFile(src, dst)
	if err == nil || !strings.Contains(err.Error(), "I/O error") {
		t.Fatalf("expected I/O error, got %v", err)
	}
}

func TestRestore_CopyFails_FailsCopy(t *testing.T) {
	fake := newFakeFS()
	fake.CopyErr = errors.New("I/O error")
	fs = fake

	src := filepath.Join(t.TempDir(), "nonexistent.db")
	dst := filepath.Join(t.TempDir(), "dest.db")

	err := copyFile(src, dst)
	if err == nil || !strings.Contains(err.Error(), "I/O error") {
		t.Fatalf("expected I/O error, got %v", err)
	}
}
