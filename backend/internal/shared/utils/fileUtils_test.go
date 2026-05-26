package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDoesNotLookLikeFilePath(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid simple path",
			input:   "file.txt",
			wantErr: false,
		},
		{
			name:    "valid nested path",
			input:   "dir/subdir/file.txt",
			wantErr: false,
		},
		{
			name:    "valid path with hyphens",
			input:   "my-app-file.yaml",
			wantErr: false,
		},
		{
			name:    "valid path with underscores",
			input:   "my_app_file.yaml",
			wantErr: false,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
			errMsg:  "file path cannot be empty",
		},
		{
			name:    "whitespace only",
			input:   "   ",
			wantErr: true,
			errMsg:  "file path cannot be empty",
		},
		{
			name:    "path with parent directory traversal",
			input:   "../etc/passwd",
			wantErr: true,
			errMsg:  "file path cannot contain '..'",
		},
		{
			name:    "path with double dots in middle",
			input:   "dir/../file.txt",
			wantErr: true,
			errMsg:  "file path cannot contain '..'",
		},
		{
			name:    "path with double dots at start",
			input:   "../../etc/shadow",
			wantErr: true,
			errMsg:  "file path cannot contain '..'",
		},
		{
			name:    "path with less than character",
			input:   "file<name>.txt",
			wantErr: true,
			errMsg:  "file path contains invalid character: <",
		},
		{
			name:    "path with greater than character",
			input:   "file>name.txt",
			wantErr: true,
			errMsg:  "file path contains invalid character: >",
		},
		{
			name:    "path with colon",
			input:   "file:name.txt",
			wantErr: true,
			errMsg:  "file path contains invalid character: :",
		},
		{
			name:    "path with double quote",
			input:   "file\"name\".txt",
			wantErr: true,
			errMsg:  "file path contains invalid character: \"",
		},
		{
			name:    "path with pipe",
			input:   "file|name.txt",
			wantErr: true,
			errMsg:  "file path contains invalid character: |",
		},
		{
			name:    "path with question mark",
			input:   "file?name.txt",
			wantErr: true,
			errMsg:  "file path contains invalid character: ?",
		},
		{
			name:    "path with asterisk",
			input:   "file*name.txt",
			wantErr: true,
			errMsg:  "file path contains invalid character: *",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := DoesNotLookLikeFilePath(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("DoesNotLookLikeFilePath() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errMsg != "" && err.Error() != tt.errMsg {
				t.Errorf("DoesNotLookLikeFilePath() error message = %q, want %q", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestIsPathWithinBase(t *testing.T) {
	// Create temporary directories for testing
	tmpDir := t.TempDir()
	baseDir := filepath.Join(tmpDir, "base")
	if err := os.MkdirAll(baseDir, 0o750); err != nil {
		t.Fatalf("failed to create base directory: %v", err)
	}

	tests := []struct {
		name     string
		baseDir  string
		filePath string
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "file directly in base directory",
			baseDir:  baseDir,
			filePath: filepath.Join(baseDir, "file.txt"),
			wantErr:  false,
		},
		{
			name:     "file in subdirectory of base",
			baseDir:  baseDir,
			filePath: filepath.Join(baseDir, "subdir", "file.txt"),
			wantErr:  false,
		},
		{
			name:     "file deeply nested in base",
			baseDir:  baseDir,
			filePath: filepath.Join(baseDir, "a", "b", "c", "file.txt"),
			wantErr:  false,
		},
		{
			name:     "relative path within base (with ./)",
			baseDir:  baseDir,
			filePath: filepath.Join(baseDir, ".", "file.txt"),
			wantErr:  false,
		},
		{
			name:     "file outside base directory",
			baseDir:  baseDir,
			filePath: filepath.Join(tmpDir, "other", "file.txt"),
			wantErr:  true,
			errMsg:   "path traversal detected: file path is outside base directory",
		},
		{
			name:     "file in parent directory of base",
			baseDir:  baseDir,
			filePath: filepath.Join(tmpDir, "file.txt"),
			wantErr:  true,
			errMsg:   "path traversal detected: file path is outside base directory",
		},
		{
			name:     "relative path traversing out of base",
			baseDir:  baseDir,
			filePath: filepath.Join(baseDir, "subdir", "..", "..", "file.txt"),
			wantErr:  true,
			errMsg:   "path traversal detected: file path is outside base directory",
		},
		{
			name:     "base directory itself",
			baseDir:  baseDir,
			filePath: baseDir,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := IsPathWithinBase(tt.baseDir, tt.filePath)
			if (err != nil) != tt.wantErr {
				t.Errorf("IsPathWithinBase() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errMsg != "" && err.Error() != tt.errMsg {
				t.Errorf("IsPathWithinBase() error message = %q, want %q", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestIsPathWithinBaseInvalidInput(t *testing.T) {
	tests := []struct {
		name     string
		baseDir  string
		filePath string
		wantErr  bool
	}{
		{
			name:     "invalid base directory path",
			baseDir:  "/nonexistent/base/path/that/is/invalid\x00",
			filePath: "/some/file.txt",
			wantErr:  true,
		},
		{
			name:     "invalid file path",
			baseDir:  "/tmp",
			filePath: "/some/file/path\x00",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := IsPathWithinBase(tt.baseDir, tt.filePath)
			if (err != nil) != tt.wantErr {
				t.Errorf("IsPathWithinBase() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
