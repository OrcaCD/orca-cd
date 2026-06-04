package provider

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"
)

const defaultGotifyPriority = 5

type GotifyProvider struct{}

type gotifyConfig struct {
	ServerURL  string `json:"serverUrl"`
	AppToken   string `json:"appToken"`
	Priority   *int   `json:"priority"`
	CustomPath string `json:"customPath"`
}

func (GotifyProvider) BuildShouterrrUrls(rawConfig string) ([]string, error) {
	trimmed, err := normalizeRawConfig(rawConfig)
	if err != nil {
		return nil, err
	}

	cfg, err := decodeConfigJSON[gotifyConfig](trimmed, "invalid JSON gotify config")
	if err != nil {
		return nil, err
	}

	serverURL := strings.TrimSpace(cfg.ServerURL)
	if serverURL == "" {
		return nil, errors.New("gotify config requires serverUrl")
	}

	appToken := strings.TrimSpace(cfg.AppToken)
	if appToken == "" {
		return nil, errors.New("gotify config requires appToken")
	}

	parsedServerURL, err := parseGotifyServerURL(serverURL)
	if err != nil {
		return nil, err
	}

	priority := defaultGotifyPriority
	if cfg.Priority != nil {
		priority = *cfg.Priority
	}
	if priority < -2 || priority > 10 {
		return nil, errors.New("gotify priority must be between -2 and 10")
	}

	gotifyURL := url.URL{
		Scheme: "gotify",
		Host:   parsedServerURL.Host,
		Path:   joinGotifyPath(parsedServerURL.Path, cfg.CustomPath, appToken),
	}

	query := url.Values{}
	query.Set("priority", strconv.Itoa(priority))
	if parsedServerURL.Scheme == "http" {
		query.Set("disabletls", "yes")
	}
	gotifyURL.RawQuery = query.Encode()

	return []string{gotifyURL.String()}, nil
}

func parseGotifyServerURL(rawURL string) (*url.URL, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid gotify server URL: %w", err)
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, errors.New("gotify server URL must use http or https")
	}
	if parsedURL.Host == "" {
		return nil, errors.New("gotify server URL must include a host")
	}
	if parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return nil, errors.New("gotify server URL must not include a query string or fragment")
	}

	return parsedURL, nil
}

func joinGotifyPath(serverPath, customPath, appToken string) string {
	parts := []string{strings.TrimSpace(serverPath), strings.TrimSpace(customPath), appToken}
	joinedParts := make([]string, 0, len(parts))

	for _, part := range parts {
		trimmed := strings.Trim(part, "/")
		if trimmed != "" {
			joinedParts = append(joinedParts, trimmed)
		}
	}

	return "/" + path.Join(joinedParts...)
}
