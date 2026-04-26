package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/OrcaCD/orca-cd/internal/version"
)

const DefaultTimeout = 15 * time.Second

var safeTransport = &http.Transport{
	DialContext:           dialer.DialContext,
	ForceAttemptHTTP2:     true,
	MaxIdleConns:          100,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: 2 * time.Second,
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	host := req.URL.Hostname()
	if IsPrivateIP(host) && !isInternalIPAllowed(host) {
		return fmt.Errorf("SSRF detected: redirect to %s is prohibited", host)
	}
	return nil
}

var Default = &http.Client{
	Timeout:       DefaultTimeout,
	Transport:     safeTransport,
	CheckRedirect: checkRedirect,
}

func UserAgent() string {
	return "OrcaCD/" + version.Version
}

func NewRequest(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent())
	return req, nil
}

func Get(ctx context.Context, url string) (*http.Response, error) {
	req, err := NewRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return Default.Do(req)
}
