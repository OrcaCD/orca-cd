package provider

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type WebhookProvider struct{}

type webhookConfig struct {
	WebhookURL string            `json:"webhookUrl"`
	Headers    map[string]string `json:"headers"`
}

func (WebhookProvider) BuildShouterrrUrls(rawConfig string) ([]string, error) {
	trimmed, err := normalizeRawConfig(rawConfig)
	if err != nil {
		return nil, err
	}

	cfg, err := decodeConfigJSON[webhookConfig](trimmed, "invalid JSON webhook config")
	if err != nil {
		return nil, err
	}

	webhookURL := strings.TrimSpace(cfg.WebhookURL)
	if webhookURL == "" {
		return nil, errors.New("webhook config requires webhookUrl")
	}

	parsedWebhookURL, err := parseWebhookURL(webhookURL)
	if err != nil {
		return nil, err
	}

	serviceURL := *parsedWebhookURL
	query := serviceURL.Query()
	query.Set("method", "POST")
	query.Set("template", "json")
	if parsedWebhookURL.Scheme == "http" {
		query.Set("disabletls", "yes")
	}

	for rawName, rawValue := range cfg.Headers {
		headerName := strings.TrimSpace(rawName)
		if headerName == "" {
			return nil, errors.New("webhook header name must not be empty")
		}
		if !isValidHeaderName(headerName) {
			return nil, fmt.Errorf("invalid webhook header name %q", headerName)
		}

		query.Set("@"+headerName, strings.TrimSpace(rawValue))
	}

	serviceURL.Scheme = "generic"
	serviceURL.RawQuery = query.Encode()

	return []string{serviceURL.String()}, nil
}

func parseWebhookURL(rawURL string) (*url.URL, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid webhook URL: %w", err)
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, errors.New("webhook URL must use http or https")
	}
	if parsedURL.Host == "" {
		return nil, errors.New("webhook URL must include a host")
	}
	if parsedURL.Fragment != "" {
		return nil, errors.New("webhook URL must not include a fragment")
	}

	return parsedURL, nil
}

func isValidHeaderName(headerName string) bool {
	for _, char := range headerName {
		if !isHeaderNameChar(char) {
			return false
		}
	}

	return true
}

func isHeaderNameChar(char rune) bool {
	switch {
	case 'a' <= char && char <= 'z':
		return true
	case 'A' <= char && char <= 'Z':
		return true
	case '0' <= char && char <= '9':
		return true
	}

	switch char {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	default:
		return false
	}
}
