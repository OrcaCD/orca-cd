package provider

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/nicholas-fedor/shoutrrr/pkg/services/chat/teams"
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
		return nil, errors.New("teams host must be a valid HTTPS Teams webhook URL")
	}

	serviceConfig, err := teams.ConfigFromWebhookURL(webhookURL)
	if err != nil {
		return nil, fmt.Errorf("invalid teams host: %w", err)
	}
	serviceConfig.Title = strings.TrimSpace(cfg.Title)

	serviceURL := serviceConfig.GetURL()
	if serviceURL == nil {
		return nil, errors.New("invalid teams host")
	}

	return []string{serviceURL.String()}, nil
}
