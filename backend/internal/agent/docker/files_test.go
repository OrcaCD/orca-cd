package docker

import (
	"os"
	"path/filepath"
	"runtime"
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

func openTestRoot(t *testing.T, dir string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root
}

func TestWriteRootFileAtomic_CreatesFileAndParentDirectories(t *testing.T) {
	dir := t.TempDir()
	root := openTestRoot(t, dir)

	if err := writeRootFileAtomic(root, "env/nested/app.env", []byte("A=1\n")); err != nil {
		t.Fatalf("writeRootFileAtomic: %v", err)
	}

	target := filepath.Join(dir, "env", "nested", "app.env")
	//nolint:gosec // path is built from t.TempDir()
	if content, err := os.ReadFile(target); err != nil || string(content) != "A=1\n" {
		t.Fatalf("expected file content, got %q (err=%v)", content, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(target)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("expected mode 0600, got %v", info.Mode().Perm())
		}
	}
	entries, err := os.ReadDir(filepath.Dir(target))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected no leftover temporary files, got %v", entries)
	}
}

func TestWriteRootFileAtomic_OverwritesExistingFile(t *testing.T) {
	dir := t.TempDir()
	root := openTestRoot(t, dir)

	for _, content := range []string{"old", "new"} {
		if err := writeRootFileAtomic(root, ".env", []byte(content)); err != nil {
			t.Fatalf("writeRootFileAtomic: %v", err)
		}
	}

	//nolint:gosec // path is built from t.TempDir()
	if content, _ := os.ReadFile(filepath.Join(dir, ".env")); string(content) != "new" {
		t.Fatalf("expected the file to be overwritten, got %q", content)
	}
}

func TestWriteRootFileAtomic_ReplacesSymlinkInsteadOfFollowingIt(t *testing.T) {
	dir := t.TempDir()
	root := openTestRoot(t, dir)
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("original"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	target := filepath.Join(dir, ".env")
	if err := os.Symlink(outside, target); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}

	if err := writeRootFileAtomic(root, ".env", []byte("new")); err != nil {
		t.Fatalf("writeRootFileAtomic: %v", err)
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
}

func TestWriteRootFileAtomic_RejectsSymlinkedDirectoryOutsideRoot(t *testing.T) {
	dir := t.TempDir()
	root := openTestRoot(t, dir)
	outsideDir := t.TempDir()
	if err := os.Symlink(outsideDir, filepath.Join(dir, "link")); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}

	if err := writeRootFileAtomic(root, "link/app.env", []byte("secret")); err == nil {
		t.Fatal("expected a write through a symlink leaving the root to be rejected")
	}

	entries, err := os.ReadDir(outsideDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected nothing to be written outside the root, got %v", entries)
	}
}

func TestWriteRootFileAtomic_RejectsPathTraversal(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "root")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	root := openTestRoot(t, dir)

	if err := writeRootFileAtomic(root, "../escaped.env", []byte("secret")); err == nil {
		t.Fatal("expected a path leaving the root to be rejected")
	}
	if _, err := os.Stat(filepath.Join(base, "escaped.env")); err == nil {
		t.Fatal("expected nothing to be written outside the root")
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

func TestFileHelpers_Errors(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Run("ensureDeploymentDir below a file", func(t *testing.T) {
		if err := ensureDeploymentDir(filepath.Join(blocker, "app")); err == nil {
			t.Fatal("expected an error")
		}
	})

	t.Run("writeFileAtomic into a missing directory", func(t *testing.T) {
		if err := writeFileAtomic(filepath.Join(dir, "missing", "app.env"), []byte("A=1")); err == nil {
			t.Fatal("expected an error")
		}
	})

	t.Run("writeRootFileAtomic below a file", func(t *testing.T) {
		root := openTestRoot(t, dir)
		if err := writeRootFileAtomic(root, "blocker/sub/app.env", []byte("A=1")); err == nil {
			t.Fatal("expected an error")
		}
	})

	t.Run("readDeploymentFile in a missing directory", func(t *testing.T) {
		if _, ok := readDeploymentFile(filepath.Join(dir, "missing"), ".env"); ok {
			t.Fatal("expected the file not to be read")
		}
	})
}
