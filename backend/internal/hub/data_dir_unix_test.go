//go:build !windows

package hub

import (
	"os"
	"strings"
	"testing"
)

func TestCheckWritable_ReadOnlyDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: chmod has no effect when running as root")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o400); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	// Restore before t.TempDir cleanup so the directory can be removed.
	// t.Cleanup runs in LIFO order, so this runs before the TempDir cleanup.
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // restore perms so t.TempDir cleanup can remove it
			t.Errorf("cleanup chmod: %v", err)
		}
	})

	err := checkWritable(dir)
	if err == nil {
		t.Fatal("expected error for read-only directory, got nil")
	}
	if !strings.Contains(err.Error(), "not writable") {
		t.Errorf("error %q does not contain expected message", err)
	}
}
