package utils

import (
	"os"
	"strings"
)

// ParseBool reports whether s is a truthy value: 1, t, true, y, yes or on
// (case-insensitive, surrounding whitespace ignored). Anything else is false.
func ParseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "t", "true", "y", "yes", "on":
		return true
	default:
		return false
	}
}

// EnvBool returns ParseBool of the environment variable key.
func EnvBool(key string) bool {
	return ParseBool(os.Getenv(key))
}
