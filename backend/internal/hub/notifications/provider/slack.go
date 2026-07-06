package provider

import (
	"errors"
	"net/url"
	"strings"
)

type SlackProvider struct{}

type slackConfig struct {
	WebhookURL string `json:"webhookUrl"`
	Title      string `json:"title"`
}

func (SlackProvider) BuildShoutrrrUrls(rawConfig string) ([]string, error) {
	trimmed, err := normalizeRawConfig(rawConfig)
	if err != nil {
		return nil, err
	}

	cfg, err := decodeConfigJSON[slackConfig](trimmed, "invalid JSON slack config")
	if err != nil {
		return nil, err
	}

	webhookURL := strings.TrimSpace(cfg.WebhookURL)
	if webhookURL == "" {
		return nil, errors.New("slack config requires webhookUrl")
	}

	token, err := parseSlackWebhookToken(webhookURL)
	if err != nil {
		return nil, err
	}

	query := url.Values{}
	if v := strings.TrimSpace(cfg.Title); v != "" {
		query.Set("title", v)
	}

	slackURL := url.URL{
		Scheme:   "slack",
		User:     url.UserPassword("hook", token),
		Host:     "webhook",
		RawQuery: query.Encode(),
	}

	return []string{slackURL.String()}, nil
}

func parseSlackWebhookToken(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != "hooks.slack.com" {
		return "", errors.New("invalid slack webhook URL")
	}

	pathWithoutPrefix := strings.TrimPrefix(parsed.Path, "/services/")
	if pathWithoutPrefix == parsed.Path {
		return "", errors.New("invalid slack webhook URL: path must start with /services/")
	}

	parts := strings.Split(strings.TrimRight(pathWithoutPrefix, "/"), "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", errors.New("invalid slack webhook URL: expected format https://hooks.slack.com/services/T/B/token")
	}

	return parts[0] + "-" + parts[1] + "-" + parts[2], nil
}
