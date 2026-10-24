package docker

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Containers can bind-mount their deployment directory and plant symlinks in
// it, so the agent never writes or reads through a path a container may have
// replaced.

// ensureDeploymentDir creates dir, refusing to reuse a symlink or any other
// non-directory planted in its place.
func ensureDeploymentDir(dir string) error {
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return os.MkdirAll(dir, 0o750)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	return nil
}

// writeFileAtomic writes data with mode 0600 to a new temporary file next to
// path and renames it into place. Renaming replaces a symlink at path instead
// of following it.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// readDeploymentFile reads name from dir without following symlinks out of dir.
// It reports false if the file is missing or cannot be read safely, e.g.
// because it is a symlink pointing outside dir.
func readDeploymentFile(dir, name string) ([]byte, bool) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, false
	}
	defer func() { _ = root.Close() }()

	data, err := root.ReadFile(name)
	if err != nil {
		return nil, false
	}
	return data, true
}
