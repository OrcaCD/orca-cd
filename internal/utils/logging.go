package utils

import "strings"

// ShoudLogJSON checks if JSON logging should be used based on environment variables and command line arguments
func ShoudLogJSON(environ []string, args []string) bool {
	// Check for production environment
	for _, env := range environ {
		if strings.HasPrefix(env, "GIN_MODE=") && strings.Contains(env, "release") {
			return true
		}
		if strings.HasPrefix(env, "LOG_FORMAT=") && strings.Contains(env, "json") {
			return true
		}
	}

	// Check for specific commands that should use JSON (e.g., health check for machine parsing)
	for _, arg := range args {
		if arg == "health" || arg == "--json" {
			return true
		}
	}

	return false
}
