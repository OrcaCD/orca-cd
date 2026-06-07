package provider

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type CustomProvider struct{}

type customConfig struct {
	URL string `json:"url"`
}

func (CustomProvider) BuildShoutrrrUrls(rawConfig string) ([]string, error) {
	trimmed, err := normalizeRawConfig(rawConfig)
	if err != nil {
		return nil, err
	}

	customURL := trimmed
	if strings.HasPrefix(trimmed, "{") {
		cfg, err := decodeConfigJSON[customConfig](trimmed, "invalid JSON custom notification config")
		if err != nil {
			return nil, err
		}
		customURL = cfg.URL
	}

	customURL = strings.TrimSpace(customURL)
	if customURL == "" {
		return nil, errors.New("custom notification config requires url")
	}

	parsedURL, err := url.Parse(customURL)
	if err != nil {
		return nil, fmt.Errorf("invalid custom Shoutrrr URL: %w", err)
	}
	if parsedURL.Scheme == "" {
		return nil, errors.New("custom Shoutrrr URL must include a scheme")
	}

	return []string{customURL}, nil
}
