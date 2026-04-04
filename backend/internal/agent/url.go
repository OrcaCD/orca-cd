package agent

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const defaultWSPath = "/api/v1/ws"

func parseHubURL(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("HUB_URL is required")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("HUB_URL is not a valid URL: %w", err)
	}

	if u.Host == "" {
		return "", errors.New("HUB_URL must include a scheme and host (e.g. https://hub.example.com)")
	}

	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "http", "ws":
		scheme = "ws"
	case "https", "wss":
		scheme = "wss"
	default:
		return "", fmt.Errorf("HUB_URL has unsupported scheme %q, expected http, https, ws, or wss", u.Scheme)
	}

	if u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("HUB_URL must not include a query string or fragment")
	}

	path := u.Path
	if path == "" || path == "/" {
		path = defaultWSPath
	}

	return scheme + "://" + u.Host + path, nil
}
