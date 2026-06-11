package provider

import (
	"net/url"
	"strings"
	"testing"

	"github.com/nicholas-fedor/shoutrrr"
)

const validTeamsWebhookURL = "https://contoso.webhook.office.com/webhookb2/11111111-4444-4444-8444-cccccccccccc@22222222-4444-4444-8444-cccccccccccc/IncomingWebhook/33333333012222222222333333333344/44444444-4444-4444-8444-cccccccccccc/V2ESyij_gAljSoUQHvZoZYzlpAoAXExyOl26dlf1xHEx05"

func TestTeamsProviderBuildShoutrrrUrls(t *testing.T) {
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
			wantErr: "invalid JSON teams config",
		},
		{
			name:    "missing host",
			raw:     `{"title":"Deploy done"}`,
			wantErr: "teams config requires host",
		},
		{
			name:    "non-https scheme must be rejected",
			raw:     `{"host":"http://contoso.webhook.office.com/webhookb2/11111111-4444-4444-8444-cccccccccccc@22222222-4444-4444-8444-cccccccccccc/IncomingWebhook/33333333012222222222333333333344/44444444-4444-4444-8444-cccccccccccc/V2ESyij_gAljSoUQHvZoZYzlpAoAXExyOl26dlf1xHEx05"}`,
			wantErr: "teams host must be a valid HTTPS URL",
		},
		{
			name:    "missing host",
			raw:     `{"host":"https:///webhookb2/11111111-4444-4444-8444-cccccccccccc@22222222-4444-4444-8444-cccccccccccc/IncomingWebhook/33333333012222222222333333333344/44444444-4444-4444-8444-cccccccccccc/V2ESyij_gAljSoUQHvZoZYzlpAoAXExyOl26dlf1xHEx05"}`,
			wantErr: "teams host must include a host",
		},
		{
			name:    "fragment must be rejected",
			raw:     `{"host":"https://contoso.webhook.office.com/webhookb2/11111111-4444-4444-8444-cccccccccccc@22222222-4444-4444-8444-cccccccccccc/IncomingWebhook/33333333012222222222333333333344/44444444-4444-4444-8444-cccccccccccc/V2ESyij_gAljSoUQHvZoZYzlpAoAXExyOl26dlf1xHEx05#fragment"}`,
			wantErr: "teams host must not include a fragment",
		},
		{ //nolint:gosec
			name:    "direct target string instead of json",
			raw:     "teams://11111111-4444-4444-8444-cccccccccccc@22222222-4444-4444-8444-cccccccccccc/33333333012222222222333333333344/44444444-4444-4444-8444-cccccccccccc/V2ESyij_gAljSoUQHvZoZYzlpAoAXExyOl26dlf1xHEx05?host=contoso.webhook.office.com",
			wantErr: "invalid JSON teams config",
		},
		{ //nolint:gosec
			name:    "json array instead of object",
			raw:     `[{"host":"` + validTeamsWebhookURL + `"}]`,
			wantErr: "invalid JSON teams config",
		},
	}

	provider := TeamsProvider{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := provider.BuildShoutrrrUrls(tt.raw)
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
				t.Fatalf("BuildShoutrrrUrls() error = %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("expected one URL, got %v", got)
			}
			if !strings.HasPrefix(got[0], "teams+https://") {
				t.Fatalf("expected teams+https URL, got %q", got[0])
			}
		})
	}
}

func TestTeamsProviderBuildsStructuredURL(t *testing.T) {
	provider := TeamsProvider{}

	urls, err := provider.BuildShoutrrrUrls(`{"host":"` + validTeamsWebhookURL + `","title":"Deploy done"}`)
	if err != nil {
		t.Fatalf("BuildShoutrrrUrls() error = %v", err)
	}
	if len(urls) != 1 {
		t.Fatalf("expected 1 URL, got %d", len(urls))
	}

	parsed, err := url.Parse(urls[0])
	if err != nil {
		t.Fatalf("failed to parse URL %q: %v", urls[0], err)
	}

	if parsed.Scheme != "teams" {
		t.Fatalf("expected scheme teams, got %q", parsed.Scheme)
	}
	if parsed.Query().Get("host") != validTeamsWebhookURL {
		t.Fatalf("expected host query parameter, got %q", parsed.Query().Get("host"))
	}

	query := parsed.Query()
	if query.Get("title") != "Deploy done" {
		t.Fatalf("expected title query parameter, got %q", query.Get("title"))
	}

	if _, err := shoutrrr.CreateSender(urls...); err != nil {
		t.Fatalf("expected Shoutrrr to accept Teams URL: %v", err)
	}
}

func TestTeamsProviderMinimalConfig(t *testing.T) {
	provider := TeamsProvider{}

	urls, err := provider.BuildShoutrrrUrls(`{"host":"` + validTeamsWebhookURL + `"}`)
	if err != nil {
		t.Fatalf("BuildShoutrrrUrls() error = %v", err)
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
