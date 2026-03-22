package hub

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// parseAppURL validates and normalises the APP_URL value.
// It must contain a scheme and host, must not contain a path (other than a
// single leading slash which is stripped), and must not contain a query or
// fragment.
func parseAppURL(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("APP_URL is required")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("APP_URL is not a valid URL: %w", err)
	}

	if u.Scheme == "" || u.Host == "" {
		return "", errors.New("APP_URL must include a scheme and host (e.g. https://example.com)")
	}

	path := strings.TrimPrefix(u.Path, "/")
	if path != "" {
		return "", errors.New("APP_URL must not include a path")
	}

	if u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("APP_URL must not include a query string or fragment")
	}

	return u.Scheme + "://" + u.Host, nil
}
