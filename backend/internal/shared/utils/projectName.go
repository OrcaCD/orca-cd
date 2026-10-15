package utils

import (
	"fmt"
	"strings"
)

// NormalizeProjectName derives the Docker Compose project name the agent uses
// for an application name.
func NormalizeProjectName(name string) (string, error) {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	result := b.String()
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	result = strings.Trim(result, "-")
	// Docker project names must start with a letter or digit, not an underscore.
	result = strings.TrimLeft(result, "_")
	if result == "" {
		return "", fmt.Errorf("application name %q cannot be normalized to a valid project name", name)
	}
	return result, nil
}
