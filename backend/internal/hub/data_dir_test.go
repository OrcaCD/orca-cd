package hub

import (
	"os"
	"testing"
)

func TestCheckWritable_WritableDir(t *testing.T) {
	dir := t.TempDir()
	if err := checkWritable(dir); err != nil {
		t.Fatalf("expected nil for writable directory, got %v", err)
	}
}

func TestCheckWritable_NonExistentDir(t *testing.T) {
	err := checkWritable(t.TempDir() + "/does-not-exist")
	if err == nil {
		t.Fatal("expected error for non-existent directory, got nil")
	}
	if os.IsPermission(err) {
		t.Errorf("expected a non-permission error for missing directory, got: %v", err)
	}
}
