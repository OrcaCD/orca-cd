package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type directTargetsConfig struct {
	URL  string   `json:"url"`
	URLs []string `json:"urls"`
}

func parseDirectTargets(rawConfig string) ([]string, error) {
	trimmed := strings.TrimSpace(rawConfig)
	if trimmed == "" {
		return nil, errors.New("notification config is empty")
	}

	if strings.HasPrefix(trimmed, "{") {
		var cfg directTargetsConfig
		if err := json.Unmarshal([]byte(trimmed), &cfg); err != nil {
			return nil, fmt.Errorf("invalid JSON notification config: %w", err)
		}

		return normalizeTargets(append([]string{cfg.URL}, cfg.URLs...))
	}

	if strings.HasPrefix(trimmed, "[") {
		var urls []string
		if err := json.Unmarshal([]byte(trimmed), &urls); err != nil {
			return nil, fmt.Errorf("invalid JSON notification URL list: %w", err)
		}

		return normalizeTargets(urls)
	}

	parts := strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	})

	return normalizeTargets(parts)
}

func normalizeTargets(rawTargets []string) ([]string, error) {
	targets := make([]string, 0, len(rawTargets))
	seen := make(map[string]struct{}, len(rawTargets))

	for i := range rawTargets {
		target := strings.TrimSpace(rawTargets[i])
		if target == "" {
			continue
		}
		if _, exists := seen[target]; exists {
			continue
		}
		seen[target] = struct{}{}
		targets = append(targets, target)
	}

	if len(targets) == 0 {
		return nil, errors.New("notification config does not contain any targets")
	}

	return targets, nil
}
