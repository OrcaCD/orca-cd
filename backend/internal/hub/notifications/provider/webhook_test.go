package provider

import (
	"net/url"
	"strings"
	"testing"
)

func TestWebhookProviderBuildShouterrrUrls(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{
			name:    "empty config",
			raw:     " \n ",
			wantErr: "notification config is empty",
		},
		{
			name:    "invalid json object",
			raw:     "{",
			wantErr: "invalid JSON webhook config",
		},
		{
			name:    "missing webhookUrl",
			raw:     `{"method":"POST"}`,
			wantErr: "webhook config requires webhookUrl",
		},
		{
			name:    "unsupported URL scheme",
			raw:     `{"webhookUrl":"ftp://example.com/hook"}`,
			wantErr: "webhook URL must use http or https",
		},
		{
			name:    "missing host",
			raw:     `{"webhookUrl":"https:///hook"}`,
			wantErr: "webhook URL must include a host",
		},
		{
			name:    "unsupported method",
			raw:     `{"webhookUrl":"https://example.com/hook","method":"TRACE"}`,
			wantErr: "webhook method must be one of",
		},
		{
			name:    "invalid header name",
			raw:     `{"webhookUrl":"https://example.com/hook","headers":{"Bad Header":"value"}}`,
			wantErr: "invalid webhook header name",
		},
		{
			name:    "direct target string",
			raw:     "generic://example.com/hook",
			wantErr: "invalid JSON webhook config",
		},
		{
			name:    "json array instead of object",
			raw:     `[{"webhookUrl":"https://example.com/hook"}]`,
			wantErr: "invalid JSON webhook config",
		},
	}

	provider := WebhookProvider{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := provider.BuildShouterrrUrls(tt.raw)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("BuildShouterrrUrls() error = %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("expected one URL, got %v", got)
			}
			if !strings.HasPrefix(got[0], "generic://") {
				t.Fatalf("expected generic URL, got %q", got[0])
			}
		})
	}
}

func TestWebhookProviderBuildsStructuredURL(t *testing.T) {
	provider := WebhookProvider{}

	urls, err := provider.BuildShouterrrUrls(`{"webhookUrl":"https://api.example.com/hooks/deploy?existing=value","method":"put","headers":{"Authorization":"Bearer token","X-Orca-Event":"deployment"}}`)
	if err != nil {
		t.Fatalf("BuildShouterrrUrls() error = %v", err)
	}
	if len(urls) != 1 {
		t.Fatalf("expected 1 URL, got %d", len(urls))
	}

	parsed, err := url.Parse(urls[0])
	if err != nil {
		t.Fatalf("failed to parse URL %q: %v", urls[0], err)
	}

	if parsed.Scheme != "generic" {
		t.Fatalf("expected scheme generic, got %q", parsed.Scheme)
	}
	if parsed.Host != "api.example.com" {
		t.Fatalf("expected host %q, got %q", "api.example.com", parsed.Host)
	}
	if parsed.Path != "/hooks/deploy" {
		t.Fatalf("expected path %q, got %q", "/hooks/deploy", parsed.Path)
	}

	query := parsed.Query()
	if query.Get("existing") != "value" {
		t.Fatalf("expected existing query parameter, got %q", query.Get("existing"))
	}
	if query.Get("method") != "PUT" {
		t.Fatalf("expected method query parameter, got %q", query.Get("method"))
	}
	if query.Get("template") != "json" {
		t.Fatalf("expected template query parameter, got %q", query.Get("template"))
	}
	if query.Get("@Authorization") != "Bearer token" {
		t.Fatalf("expected authorization header query parameter, got %q", query.Get("@Authorization"))
	}
	if query.Get("@X-Orca-Event") != "deployment" {
		t.Fatalf("expected custom header query parameter, got %q", query.Get("@X-Orca-Event"))
	}
}

func TestWebhookProviderHTTPURLDisablesTLS(t *testing.T) {
	provider := WebhookProvider{}

	urls, err := provider.BuildShouterrrUrls(`{"webhookUrl":"http://localhost:8123/api/webhook","method":"POST"}`)
	if err != nil {
		t.Fatalf("BuildShouterrrUrls() error = %v", err)
	}

	parsed, err := url.Parse(urls[0])
	if err != nil {
		t.Fatalf("failed to parse URL %q: %v", urls[0], err)
	}

	if parsed.Query().Get("disabletls") != "yes" {
		t.Fatalf("expected disabletls=yes for HTTP webhook, got %q", parsed.Query().Get("disabletls"))
	}
}

func TestWebhookProviderDefaultsToPost(t *testing.T) {
	provider := WebhookProvider{}

	urls, err := provider.BuildShouterrrUrls(`{"webhookUrl":"https://example.com/hook"}`)
	if err != nil {
		t.Fatalf("BuildShouterrrUrls() error = %v", err)
	}

	parsed, err := url.Parse(urls[0])
	if err != nil {
		t.Fatalf("failed to parse URL %q: %v", urls[0], err)
	}

	if parsed.Query().Get("method") != "POST" {
		t.Fatalf("expected default method POST, got %q", parsed.Query().Get("method"))
	}
}
