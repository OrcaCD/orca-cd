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

func normalizeRawConfig(rawConfig string) (string, error) {
	trimmed := strings.TrimSpace(rawConfig)
	if trimmed == "" {
		return "", errors.New("notification config is empty")
	}

	return trimmed, nil
}

func decodeConfigJSON[T any](trimmed string, invalidMessage string) (T, error) {
	var value T
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return value, fmt.Errorf("%s: %w", invalidMessage, err)
	}

	return value, nil
}

func parseDirectTargets(rawConfig string) ([]string, error) {
	trimmed, err := normalizeRawConfig(rawConfig)
	if err != nil {
		return nil, err
	}

	if strings.HasPrefix(trimmed, "{") {
		cfg, err := decodeConfigJSON[directTargetsConfig](trimmed, "invalid JSON notification config")
		if err != nil {
			return nil, err
		}

		return normalizeTargets(append([]string{cfg.URL}, cfg.URLs...))
	}

	if strings.HasPrefix(trimmed, "[") {
		urls, err := decodeConfigJSON[[]string](trimmed, "invalid JSON notification URL list")
		if err != nil {
			return nil, err
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
