package provider

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type CustomProvider struct{}

func (CustomProvider) BuildShoutrrrUrls(rawConfig string) ([]string, error) {
	customURL, err := normalizeRawConfig(rawConfig)
	if err != nil {
		return nil, err
	}

	customURL = strings.TrimSpace(customURL)
	if customURL == "" {
		return nil, errors.New("custom notification config requires Shoutrrr URL")
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
