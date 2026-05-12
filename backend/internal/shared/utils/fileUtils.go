package utils

import (
	"errors"
	"path/filepath"
	"strings"
)

type FileUtils struct{}

func DoesNotLookLikeFilePath(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("file path cannot be empty")
	}

	// Check for invalid characters that shouldn't appear in file paths
	invalidChars := []string{"<", ">", ":", "\"", "|", "?", "*"}
	for _, char := range invalidChars {
		if strings.Contains(name, char) {
			return errors.New("file path contains invalid character: " + char)
		}
	}

	// Check if path is clean and doesn't have suspicious patterns
	absPath := filepath.Clean(name)
	if absPath == "." || absPath == ".." {
		return errors.New("file path must be a valid path")
	}

	return nil
}