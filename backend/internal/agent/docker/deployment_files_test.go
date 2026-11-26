package docker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreDeselectedDeploymentFileMode(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "application")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	const name = "private.conf"
	if err := root.WriteFile(name, []byte("previous"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := deploymentFilesManifest{Paths: []string{name}, Revision: "old"}
	if err := storeDeploymentFilesManifest(dir, manifest); err != nil {
		t.Fatal(err)
	}
	update, err := prepareDeploymentFiles(root, dir, manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := update.apply(); err != nil {
		t.Fatal(err)
	}
	if err := update.restore(); err != nil {
		t.Fatal(err)
	}
	data, exists, mode, err := readDeploymentAsset(root, name, 100)
	if err != nil || !exists || string(data) != "previous" || mode != 0o600 {
		t.Fatalf("restored file = %q, exists = %t, mode = %o, error = %v", data, exists, mode, err)
	}
}
