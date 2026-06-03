package provider

import (
	"net/url"
	"strings"
	"testing"
)

func TestSlackProviderBuildShouterrrUrls(t *testing.T) {
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
			wantErr: "invalid JSON slack config",
		},
		{
			name:    "missing webhookUrl",
			raw:     `{"botName":"Orca Bot"}`,
			wantErr: "slack config requires webhookUrl",
		},
		{
			name:    "invalid webhook URL host",
			raw:     `{"webhookUrl":"https://example.com/services/T123456789/B123456789/abcdefghijklmnopqrstuvwx"}`,
			wantErr: "invalid slack webhook URL",
		},
		{
			name:    "look-alike domain ending in slack.com must be rejected",
			raw:     `{"webhookUrl":"https://evilslack.com/services/T123456789/B123456789/abcdefghijklmnopqrstuvwx"}`,
			wantErr: "invalid slack webhook URL",
		},
		{
			name:    "non-https scheme must be rejected",
			raw:     `{"webhookUrl":"http://hooks.slack.com/services/T123456789/B123456789/abcdefghijklmnopqrstuvwx"}`,
			wantErr: "invalid slack webhook URL",
		},
		{
			name:    "webhook URL missing services prefix",
			raw:     `{"webhookUrl":"https://hooks.slack.com/T123456789/B123456789/abcdefghijklmnopqrstuvwx"}`,
			wantErr: "invalid slack webhook URL: path must start with /services/",
		},
		{
			name:    "webhook URL with only two path parts",
			raw:     `{"webhookUrl":"https://hooks.slack.com/services/T123456789/B123456789"}`,
			wantErr: "invalid slack webhook URL: expected",
		},
		{ //nolint:gosec
			name:    "direct target string instead of json",
			raw:     "slack://hook:T123456789-B123456789-abcdefghijklmnopqrstuvwx@webhook",
			wantErr: "invalid JSON slack config",
		},
		{ //nolint:gosec
			name:    "json array instead of object",
			raw:     `["slack://hook:T123456789-B123456789-abcdefghijklmnopqrstuvwx@webhook"]`,
			wantErr: "invalid JSON slack config",
		},
	}

	provider := SlackProvider{}

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
			if !strings.HasPrefix(got[0], "slack://") {
				t.Fatalf("expected slack URL, got %q", got[0])
			}
		})
	}
}

func TestSlackProviderBuildsStructuredURL(t *testing.T) {
	provider := SlackProvider{}

	urls, err := provider.BuildShouterrrUrls(`{"webhookUrl":"https://hooks.slack.com/services/T123456789/B123456789/abcdefghijklmnopqrstuvwx","title":"Deploy done"}`)
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

	if parsed.Scheme != "slack" {
		t.Fatalf("expected scheme slack, got %q", parsed.Scheme)
	}
	if parsed.User == nil {
		t.Fatal("expected userinfo in URL")
	}
	if parsed.User.Username() != "hook" {
		t.Fatalf("expected username %q, got %q", "hook", parsed.User.Username())
	}
	password, _ := parsed.User.Password()
	if password != "T123456789-B123456789-abcdefghijklmnopqrstuvwx" {
		t.Fatalf("expected token in URL password, got %q", password)
	}
	if parsed.Host != "webhook" {
		t.Fatalf("expected host %q, got %q", "webhook", parsed.Host)
	}

	query := parsed.Query()
	if query.Get("title") != "Deploy done" {
		t.Fatalf("expected title query parameter, got %q", query.Get("title"))
	}
}

func TestSlackProviderMinimalConfig(t *testing.T) {
	provider := SlackProvider{}

	urls, err := provider.BuildShouterrrUrls(`{"webhookUrl":"https://hooks.slack.com/services/T123456789/B123456789/abcdefghijklmnopqrstuvwx"}`)
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

	query := parsed.Query()
	if query.Get("title") != "" {
		t.Fatalf("expected no title, got %q", query.Get("title"))
	}
}
