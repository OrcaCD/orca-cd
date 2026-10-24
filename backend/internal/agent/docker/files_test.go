package docker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomic_ReplacesSymlinkInsteadOfFollowingIt(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("original"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	target := filepath.Join(dir, composeFileName)
	if err := os.Symlink(outside, target); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	if err := writeFileAtomic(target, []byte("new")); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}

	//nolint:gosec // path is built from t.TempDir()
	if content, _ := os.ReadFile(outside); string(content) != "original" {
		t.Fatalf("expected the symlink target to stay unchanged, got %q", content)
	}
	info, err := os.Lstat(target)
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("expected %s to be replaced by a regular file, got mode %v", target, info.Mode())
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected mode 0600, got %v", info.Mode().Perm())
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected no leftover temporary files, got %v", entries)
	}
}

func TestEnsureDeploymentDir(t *testing.T) {
	base := t.TempDir()

	created := filepath.Join(base, "new")
	if err := ensureDeploymentDir(created); err != nil {
		t.Fatalf("ensureDeploymentDir: %v", err)
	}
	if info, err := os.Lstat(created); err != nil || !info.IsDir() {
		t.Fatalf("expected %s to be created as a directory", created)
	}
	if err := ensureDeploymentDir(created); err != nil {
		t.Fatalf("expected an existing directory to be reused, got %v", err)
	}

	link := filepath.Join(base, "link")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	if err := ensureDeploymentDir(link); err == nil {
		t.Fatal("expected a symlinked deployment directory to be rejected")
	}
}

func TestReadDeploymentFile(t *testing.T) {
	dir := t.TempDir()

	if _, ok := readDeploymentFile(dir, composeFileName); ok {
		t.Fatal("expected a missing file to be reported as absent")
	}

	if err := os.WriteFile(filepath.Join(dir, composeFileName), []byte("content"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if content, ok := readDeploymentFile(dir, composeFileName); !ok || string(content) != "content" {
		t.Fatalf("expected file content, got %q (ok=%v)", content, ok)
	}

	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	if content, ok := readDeploymentFile(dir, "link"); ok {
		t.Fatalf("expected a symlink pointing outside the directory not to be read, got %q", content)
	}
}
