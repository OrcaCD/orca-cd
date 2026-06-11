package provider

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type TeamsProvider struct{}

type teamsConfig struct {
	Host  string `json:"host"`
	Title string `json:"title"`
}

func (TeamsProvider) BuildShoutrrrUrls(rawConfig string) ([]string, error) {
	trimmed, err := normalizeRawConfig(rawConfig)
	if err != nil {
		return nil, err
	}

	cfg, err := decodeConfigJSON[teamsConfig](trimmed, "invalid JSON teams config")
	if err != nil {
		return nil, err
	}

	host := strings.TrimSpace(cfg.Host)
	if host == "" {
		return nil, errors.New("teams config requires host")
	}

	webhookURL, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("invalid teams host: %w", err)
	}
	if webhookURL.Scheme != "https" {
		return nil, errors.New("teams host must be a valid HTTPS URL")
	}
	if webhookURL.Host == "" {
		return nil, errors.New("teams host must include a host")
	}
	if webhookURL.Fragment != "" {
		return nil, errors.New("teams host must not include a fragment")
	}

	customURL := "teams:?host=" + url.QueryEscape(webhookURL.String())

	if title := strings.TrimSpace(cfg.Title); title != "" {
		parsedCustomURL, err := url.Parse(customURL)
		if err != nil {
			return nil, fmt.Errorf("invalid teams host: %w", err)
		}

		query := parsedCustomURL.Query()
		query.Set("title", title)
		parsedCustomURL.RawQuery = query.Encode()
		customURL = parsedCustomURL.String()
	}

	return []string{customURL}, nil
}
