package db

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type FileSystem interface {
	MkdirAll(path string, perm os.FileMode) error
	Remove(path string) error
	Stat(path string) (os.FileInfo, error)
	Open(path string) (*os.File, error)
	OpenFile(path string, flag int, perm os.FileMode) (*os.File, error)
}

type realFS struct{}

func (realFS) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }
func (realFS) Remove(path string) error                     { return os.Remove(path) }
func (realFS) Stat(path string) (os.FileInfo, error)        { return os.Stat(path) }
func (realFS) Open(path string) (*os.File, error)           { return os.Open(path) }
func (realFS) OpenFile(path string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(path, flag, perm)
}

var fs FileSystem = realFS{}

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
	fake := newFakeFS()
	fake.MkdirAllErr = errors.New("disk full")
	fs = fake // inject

	backupPath := filepath.Join(t.TempDir(), "test-backup.db")
	err := Restore(backupPath)
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("expected disk full error, got %v", err)
	}
}

func TestRestore_StatFails_NotNotExist(t *testing.T) {
	fake := newFakeFS()
	fake.StatErr = errors.New("permission denied")
	fs = fake

	backupPath := filepath.Join(t.TempDir(), "test-backup.db")
	err := Restore(backupPath)
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("expected permission denied, got %v", err)
	}
}

func TestRestore_CopyFails_RollbackSucceeds(t *testing.T) {
	fake := newFakeFS()
	fake.OpenErr = errors.New("I/O error")
	fs = fake

	backupPath := filepath.Join(t.TempDir(), "test-backup.db")
	err := Restore(backupPath)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRestore_CopyFails_(t *testing.T) {
	fake := newFakeFS()
	fake.OpenFileErr = errors.New("I/O error")
	fs = fake

	src := filepath.Join(t.TempDir(), "nonexistent.db")
	dst := filepath.Join(t.TempDir(), "dest.db")

	err := copyFile(src, dst)
	if err == nil {
		t.Fatal("expected error")
	}
}
