package httpclient

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/OrcaCD/orca-cd/internal/version"
)

const DefaultTimeout = 15 * time.Second

var Default = &http.Client{
	Timeout: DefaultTimeout,
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
