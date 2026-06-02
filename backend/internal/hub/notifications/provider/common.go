package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

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
