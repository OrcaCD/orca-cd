package utils

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

type FileUtils struct{}

// DoesNotLookLikeFilePath validates that a path is safe and doesn't contain path traversal sequences.
func DoesNotLookLikeFilePath(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("file path cannot be empty")
	}

	// Prevent path traversal attacks
	if strings.Contains(name, "..") {
		return errors.New("file path cannot contain '..'")
	}

	// Check for invalid characters that shouldn't appear in file paths
	invalidChars := []string{"<", ">", ":", "\"", "|", "?", "*"}
	for _, char := range invalidChars {
		if strings.Contains(name, char) {
			return errors.New("file path contains invalid character: " + char)
		}
	}

	return nil
}

// IsPathWithinBase ensures that a file path stays within a base directory.
// This prevents directory traversal attacks even with symlinks and relative paths.
func IsPathWithinBase(basePath, filePath string) error {
	absBase, err := filepath.Abs(basePath)
	if err != nil {
		return fmt.Errorf("invalid base path: %w", err)
	}

	absFile, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("invalid file path: %w", err)
	}

	// Ensure the file path is within the base directory
	if !strings.HasPrefix(absFile, absBase) {
		return errors.New("path traversal detected: file path is outside base directory")
	}

	return nil
}